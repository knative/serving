/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	ingressinformer "github.com/knative/serving/pkg/client/injection/informers/networking/v1alpha1/ingress"
	listers "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	"knative.dev/pkg/apis/istio/v1alpha3"
	gatewayinformer "knative.dev/pkg/client/injection/informers/istio/v1alpha3/gateway"
	virtualserviceinformer "knative.dev/pkg/client/injection/informers/istio/v1alpha3/virtualservice"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"

	istiolisters "knative.dev/pkg/client/listers/istio/v1alpha3"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	secretinformer "knative.dev/pkg/injection/informers/kubeinformers/corev1/secret"
	"knative.dev/pkg/tracker"

	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/ingress/config"
	"github.com/knative/serving/pkg/reconciler/ingress/resources"
	"go.uber.org/zap"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "ingress-controller"
)

type Reconciler struct {
	*BaseIngressReconciler
	ingressLister listers.IngressLister
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// BaseIngressReconciler is the conmon struct for InjectReconciles
type BaseIngressReconciler struct {
	*reconciler.Base

	// listers index properties about resources
	VirtualServiceLister istiolisters.VirtualServiceLister
	GatewayLister        istiolisters.GatewayLister
	SecretLister         corev1listers.SecretLister
	ConfigStore          reconciler.ConfigStore

	Tracker tracker.Interface
}

// NewBaseIngressReconciler creates a new BaseIngressReconciler
func NewBaseIngressReconciler(ctx context.Context, agentName string, cmw configmap.Watcher) *BaseIngressReconciler {
	virtualServiceInformer := virtualserviceinformer.Get(ctx)
	gatewayInformer := gatewayinformer.Get(ctx)
	secretInformer := secretinformer.Get(ctx)

	base := &BaseIngressReconciler{
		Base:                 reconciler.NewBase(ctx, agentName, cmw),
		VirtualServiceLister: virtualServiceInformer.Lister(),
		GatewayLister:        gatewayInformer.Lister(),
		SecretLister:         secretInformer.Lister(),
	}
	return base
}

// newInitializer creates an Ingress Reconciler and returns ReconcilerInitializer
func newInitializer(ctx context.Context, cmw configmap.Watcher) ReconcilerInitializer {
	ingressInformer := ingressinformer.Get(ctx)
	r := &Reconciler{
		BaseIngressReconciler: NewBaseIngressReconciler(ctx, controllerAgentName, cmw),
		ingressLister:         ingressInformer.Lister(),
	}
	return r
}

// SetTracker assigns the Tracker field
func (r *Reconciler) SetTracker(tracker tracker.Interface) {
	r.Tracker = tracker
}

// Init method performs initializations to ingress reconciler
func (r *Reconciler) Init(ctx context.Context, cmw configmap.Watcher, impl *controller.Impl) {

	SetupSecretTracker(ctx, cmw, r, impl)

	r.Logger.Info("Setting up Ingress event handlers")
	ingressInformer := ingressinformer.Get(ctx)

	myFilterFunc := reconciler.AnnotationFilterFunc(networking.IngressClassAnnotationKey, network.IstioIngressClassName, true)
	ingressHandler := cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler:    controller.HandleAll(impl.Enqueue),
	}
	ingressInformer.Informer().AddEventHandler(ingressHandler)

	virtualServiceInformer := virtualserviceinformer.Get(ctx)
	virtualServiceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler:    controller.HandleAll(impl.EnqueueControllerOf),
	})

	r.Logger.Info("Setting up ConfigMap receivers")
	configsToResync := []interface{}{
		&config.Istio{},
		&network.Config{},
	}
	resyncIngressesOnConfigChange := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
		controller.SendGlobalUpdates(ingressInformer.Informer(), ingressHandler)
	})
	configStore := config.NewStore(r.Logger.Named("config-store"), resyncIngressesOnConfigChange)
	configStore.WatchConfigs(cmw)
	r.ConfigStore = configStore

}

// SetupSecretTracker initializes Secret Tracker
func SetupSecretTracker(ctx context.Context, cmw configmap.Watcher, init ReconcilerInitializer, impl *controller.Impl) {

	logger := logging.FromContext(ctx)
	logger.Info("Setting up secret informer event handler")

	// Create tracker
	tracker := tracker.New(impl.EnqueueKey, controller.GetTrackerLease(ctx))
	init.SetTracker(tracker)

	// add secret event handler
	secretInformer := secretinformer.Get(ctx)
	secretInformer.Informer().AddEventHandler(controller.HandleAll(
		controller.EnsureTypeMeta(
			tracker.OnChanged,
			corev1.SchemeGroupVersion.WithKind("Secret"),
		),
	))
}

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Ingress resource
// with the current status of the resource.
func (r *Reconciler) Reconcile(ctx context.Context, key string) error {
	return ReconcileIngress(r.ConfigStore.ToContext(ctx), r, key)
}

// ReconcileIngress retrieves Ingress by key and performs reconciliation
func ReconcileIngress(ctx context.Context, ra ReconcilerAccessor, key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	logger := logging.FromContext(ctx)

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		logger.Errorf("invalid resource key: %s", key)
		return nil
	}

	// Get the Ingress resource with this namespace and name.
	original, err := ra.GetIngress(ns, name)
	if apierrs.IsNotFound(err) {
		// The resource may no longer exist, in which case we stop processing.
		logger.Errorf("ingress %q in work queue no longer exists", key)
		return nil
	} else if err != nil {
		return err
	}
	// Don't modify the informers copy
	ingress := original.DeepCopyObject().(v1alpha1.IngressAccessor)

	// Reconcile this copy of the Ingress and then write back any status
	// updates regardless of whether the reconciliation errored out.
	reconcileErr := reconcileIngress(ctx, ra, ingress)
	if equality.Semantic.DeepEqual(original.GetStatus(), ingress.GetStatus()) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else {
		if _, err = updateStatus(ra, ingress); err != nil {
			logger.Warnw("Failed to update Ingress status", zap.Error(err))
			ra.GetRecorder().Eventf(ingress, corev1.EventTypeWarning, "UpdateFailed",
				"Failed to update status for Ingress %q: %v", ingress.GetName(), err)
			return err
		}

		logger.Infof("Updated status for Ingress %q", ingress.GetName())
		ra.GetRecorder().Eventf(ingress, corev1.EventTypeNormal, "Updated",
			"Updated status for Ingress %q", ingress.GetName())
	}
	if reconcileErr != nil {
		ra.GetRecorder().Event(ingress, corev1.EventTypeWarning, "InternalError", reconcileErr.Error())
	}
	return reconcileErr
}

func reconcileIngress(ctx context.Context, ra ReconcilerAccessor, ia v1alpha1.IngressAccessor) error {
	logger := logging.FromContext(ctx)
	if ia.GetDeletionTimestamp() != nil {
		return reconcileDeletion(ctx, ra, ia)
	}

	// We may be reading a version of the object that was stored at an older version
	// and may not have had all of the assumed defaults specified.  This won't result
	// in this getting written back to the API Server, but lets downstream logic make
	// assumptions about defaulting.
	ia.SetDefaults(ctx)

	ia.GetStatus().InitializeConditions()
	logger.Infof("Reconciling ingress: %#v", ia)

	gatewayNames := gatewayNamesFromContext(ctx, ia)
	vses := resources.MakeVirtualServices(ia, gatewayNames)

	// First, create the VirtualServices.
	logger.Infof("Creating/Updating VirtualServices")
	if err := reconcileVirtualServices(ctx, ra, ia, vses); err != nil {
		// TODO(lichuqiang): should we explicitly mark the ingress as unready
		// when error reconciling VirtualService?
		return err
	}
	// As underlying network programming (VirtualService now) is stateless,
	// here we simply mark the ingress as ready if the VirtualService
	// is successfully synced.
	ia.GetStatus().MarkNetworkConfigured()
	ia.GetStatus().MarkLoadBalancerReady(getLBStatus(gatewayServiceURLFromContext(ctx, ia)))
	ia.GetStatus().ObservedGeneration = ia.GetGeneration()

	if enablesAutoTLS(ctx) {
		if !ia.IsPublic() {
			logger.Infof("Ingress %s is not public. So no need to configure TLS.", ia.GetName())
			return nil
		}

		// Add the finalizer before adding `Servers` into Gateway so that we can be sure
		// the `Servers` get cleaned up from Gateway.
		if err := ensureFinalizer(ra, ia); err != nil {
			return err
		}

		originSecrets, err := resources.GetSecrets(ia, ra.GetSecretLister())
		if err != nil {
			return err
		}
		targetSecrets := resources.MakeSecrets(ctx, originSecrets, ia)
		if err := reconcileCertSecrets(ctx, ra, ia, targetSecrets); err != nil {
			return err
		}

		for _, gatewayName := range gatewayNames {
			ns, err := resources.GatewayServiceNamespace(config.FromContext(ctx).Istio.IngressGateways, gatewayName)
			if err != nil {
				return err
			}
			desired, err := resources.MakeServers(ia, ns, originSecrets)
			if err != nil {
				return err
			}
			if err := reconcileGateway(ctx, ra, ia, gatewayName, desired); err != nil {
				return err
			}
		}
	}

	// TODO(zhiminx): Mark Route status to indicate that Gateway is configured.
	logger.Info("ClusterIngress successfully synced")
	return nil
}

func reconcileCertSecrets(ctx context.Context, ra ReconcilerAccessor, ia v1alpha1.IngressAccessor, desiredSecrets []*corev1.Secret) error {
	for _, certSecret := range desiredSecrets {
		if err := reconcileCertSecret(ctx, ra, ia, certSecret); err != nil {
			return err
		}
	}
	return nil
}

func reconcileCertSecret(ctx context.Context, ra ReconcilerAccessor, ia v1alpha1.IngressAccessor, desired *corev1.Secret) error {
	// We track the origin and desired secrets so that desired secrets could be synced accordingly when the origin TLS certificate
	// secret is refreshed.
	ra.GetTracker().Track(resources.SecretRef(desired.Namespace, desired.Name), ia)
	ra.GetTracker().Track(resources.SecretRef(desired.Labels[networking.OriginSecretNamespaceLabelKey], desired.Labels[networking.OriginSecretNameLabelKey]), ia)

	logger := logging.FromContext(ctx)
	existing, err := ra.GetSecretLister().Secrets(desired.Namespace).Get(desired.Name)
	if apierrs.IsNotFound(err) {
		_, err = ra.GetKubeClientSet().CoreV1().Secrets(desired.Namespace).Create(desired)
		if err != nil {
			logger.Errorw("Failed to create Certificate Secret", zap.Error(err))
			ra.GetRecorder().Eventf(ia, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create Secret %s/%s: %v", desired.Namespace, desired.Name, err)
			return err
		}
		ra.GetRecorder().Eventf(ia, corev1.EventTypeNormal, "Created",
			"Created Secret %s/%s", desired.Namespace, desired.Name)
	} else if err != nil {
		return err
	} else if !equality.Semantic.DeepEqual(existing.Data, desired.Data) {
		// Don't modify the informers copy
		copy := existing.DeepCopy()
		copy.Data = desired.Data
		_, err = ra.GetKubeClientSet().CoreV1().Secrets(copy.Namespace).Update(copy)
		if err != nil {
			logger.Errorw("Failed to update target secret", zap.Error(err))
			ra.GetRecorder().Eventf(ia, corev1.EventTypeWarning, "UpdateFailed",
				"Failed to update Secret %s/%s: %v", desired.Namespace, desired.Name, err)
			return err
		}
		ra.GetRecorder().Eventf(ia, corev1.EventTypeNormal, "Updated",
			"Updated Secret %s/%s", copy.Namespace, copy.Name)
	}
	return nil
}

func reconcileVirtualServices(ctx context.Context, ra ReconcilerAccessor, ia v1alpha1.IngressAccessor,
	desired []*v1alpha3.VirtualService) error {
	logger := logging.FromContext(ctx)
	// First, create all needed VirtualServices.
	kept := sets.NewString()
	for _, d := range desired {
		if err := reconcileVirtualService(ctx, ra, ia, d); err != nil {
			return err
		}
		kept.Insert(d.Name)
	}
	// Now, remove the extra ones.
	vses, err := ra.GetVirtualServiceLister().VirtualServices(resources.VirtualServiceNamespace(ia)).List(
		labels.Set(map[string]string{
			serving.RouteLabelKey:          ia.GetLabels()[serving.RouteLabelKey],
			serving.RouteNamespaceLabelKey: ia.GetLabels()[serving.RouteNamespaceLabelKey]}).AsSelector())
	if err != nil {
		logger.Errorw("Failed to get VirtualServices", zap.Error(err))
		return err
	}
	for _, vs := range vses {
		n, ns := vs.Name, vs.Namespace
		if kept.Has(n) {
			continue
		}
		if err = ra.GetSharedClientSet().NetworkingV1alpha3().VirtualServices(ns).Delete(n, &metav1.DeleteOptions{}); err != nil {
			logger.Errorw("Failed to delete VirtualService", zap.Error(err))
			return err
		}
	}
	return nil
}

func reconcileVirtualService(ctx context.Context, ra ReconcilerAccessor, ia v1alpha1.IngressAccessor,
	desired *v1alpha3.VirtualService) error {
	logger := logging.FromContext(ctx)
	ns := desired.Namespace
	name := desired.Name

	vs, err := ra.GetVirtualServiceLister().VirtualServices(ns).Get(name)
	if apierrs.IsNotFound(err) {
		_, err = ra.GetSharedClientSet().NetworkingV1alpha3().VirtualServices(ns).Create(desired)
		if err != nil {
			logger.Errorw("Failed to create VirtualService", zap.Error(err))
			ra.GetRecorder().Eventf(ia, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create VirtualService %q/%q: %v", ns, name, err)
			return err
		}
		ra.GetRecorder().Eventf(ia, corev1.EventTypeNormal, "Created",
			"Created VirtualService %q", desired.Name)
	} else if err != nil {
		return err
	} else if !metav1.IsControlledBy(vs, ia) {
		// Surface an error in the ClusterIngress's status, and return an error.
		ia.GetStatus().MarkResourceNotOwned("VirtualService", name)
		return fmt.Errorf("Ingress: %q does not own VirtualService: %q", ia.GetName(), name)
	} else if !equality.Semantic.DeepEqual(vs.Spec, desired.Spec) {
		// Don't modify the informers copy
		existing := vs.DeepCopy()
		existing.Spec = desired.Spec
		_, err = ra.GetSharedClientSet().NetworkingV1alpha3().VirtualServices(ns).Update(existing)
		if err != nil {
			logger.Errorw("Failed to update VirtualService", zap.Error(err))
			return err
		}
		ra.GetRecorder().Eventf(ia, corev1.EventTypeNormal, "Updated",
			"Updated status for VirtualService %q/%q", ns, name)
	}
	return nil
}

func reconcileDeletion(ctx context.Context, ra ReconcilerAccessor, ia v1alpha1.IngressAccessor) error {
	logger := logging.FromContext(ctx)

	// If our Finalizer is first, delete the `Servers` from Gateway for this ClusterIngress,
	// and remove the finalizer.
	if len(ia.GetFinalizers()) == 0 || ia.GetFinalizers()[0] != ra.GetFinalizer() {
		return nil
	}

	gatewayNames := gatewayNamesFromContext(ctx, ia)
	logger.Infof("Cleaning up Gateway Servers for Ingress %s", ia.GetName())
	// No desired Servers means deleting all of the existing Servers associated with the CI.
	for _, gatewayName := range gatewayNames {
		if err := reconcileGateway(ctx, ra, ia, gatewayName, []v1alpha3.Server{}); err != nil {
			return err
		}
	}

	// Update the Ingress to remove the Finalizer.
	logger.Info("Removing Finalizer")
	ia.SetFinalizers(ia.GetFinalizers()[1:])
	_, err := ra.UpdateIngress(ia)
	return err
}

// Update the Status of the Ingress.  Caller is responsible for checking
// for semantic differences before calling.
func updateStatus(ra ReconcilerAccessor, desired v1alpha1.IngressAccessor) (v1alpha1.IngressAccessor, error) {
	ingress, err := ra.GetIngress(desired.GetNamespace(), desired.GetName())
	if err != nil {
		return nil, err
	}
	// If there's nothing to update, just return.
	if reflect.DeepEqual(ingress.GetStatus(), desired.GetStatus()) {
		return ingress, nil
	}
	// Don't modify the informers copy
	existing := ingress.DeepCopyObject().(v1alpha1.IngressAccessor)
	existing.SetStatus(*desired.GetStatus())
	return ra.UpdateIngressStatus(existing)
}

func ensureFinalizer(ra ReconcilerAccessor, ia v1alpha1.IngressAccessor) error {
	finalizers := sets.NewString(ia.GetFinalizers()...)
	if finalizers.Has(ra.GetFinalizer()) {
		return nil
	}

	mergePatch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"finalizers":      append(ia.GetFinalizers(), ra.GetFinalizer()),
			"resourceVersion": ia.GetResourceVersion(),
		},
	}

	patch, err := json.Marshal(mergePatch)
	if err != nil {
		return err
	}

	_, err = ra.PatchIngress(ia.GetNamespace(), ia.GetName(), types.MergePatchType, patch)
	return err
}

func reconcileGateway(ctx context.Context, ra ReconcilerAccessor, ia v1alpha1.IngressAccessor, gatewayName string, desired []v1alpha3.Server) error {
	// TODO(zhiminx): Need to handle the scenario when deleting ClusterIngress. In this scenario,
	// the Gateway servers of the ClusterIngress need also be removed from Gateway.
	logger := logging.FromContext(ctx)
	gateway, err := ra.GetGatewayLister().Gateways(system.Namespace()).Get(gatewayName)
	if err != nil {
		// Not like VirtualService, A default gateway needs to be existed.
		// It should be installed when installing Knative.
		logger.Errorw("Failed to get Gateway.", zap.Error(err))
		return err
	}

	existing := resources.GetServers(gateway, ia)
	existingHTTPServer := resources.GetHTTPServer(gateway)
	if existingHTTPServer != nil {
		existing = append(existing, *existingHTTPServer)
	}

	desiredHTTPServer := resources.MakeHTTPServer(config.FromContext(ctx).Network.HTTPProtocol)
	if desiredHTTPServer != nil {
		desired = append(desired, *desiredHTTPServer)
	}

	if equality.Semantic.DeepEqual(existing, desired) {
		return nil
	}

	copy := gateway.DeepCopy()
	copy = resources.UpdateGateway(copy, desired, existing)
	if _, err := ra.GetSharedClientSet().NetworkingV1alpha3().Gateways(copy.Namespace).Update(copy); err != nil {
		logger.Errorw("Failed to update Gateway", zap.Error(err))
		return err
	}
	ra.GetRecorder().Eventf(ia, corev1.EventTypeNormal, "Updated",
		"Updated Gateway %q/%q", gateway.Namespace, gateway.Name)
	return nil
}

// gatewayNamesFromContext get gateway names from context
func gatewayNamesFromContext(ctx context.Context, ia v1alpha1.IngressAccessor) []string {
	gateways := []string{}
	if ia.IsPublic() {
		for _, gw := range config.FromContext(ctx).Istio.IngressGateways {
			gateways = append(gateways, gw.GatewayName)
		}
	} else {
		for _, gw := range config.FromContext(ctx).Istio.LocalGateways {
			gateways = append(gateways, gw.GatewayName)
		}
	}
	return dedup(gateways)
}

func dedup(strs []string) []string {
	existed := sets.NewString()
	unique := []string{}
	// We can't just do `sets.NewString(str)`, since we need to preserve the order.
	for _, s := range strs {
		if !existed.Has(s) {
			existed.Insert(s)
			unique = append(unique, s)
		}
	}
	return unique
}

// gatewayServiceURLFromContext return an address of a load-balancer
// that the given Ingress is exposed to, or empty string if
// none.
func gatewayServiceURLFromContext(ctx context.Context, ia v1alpha1.IngressAccessor) string {
	cfg := config.FromContext(ctx).Istio
	if len(cfg.IngressGateways) > 0 && ia.IsPublic() {
		return cfg.IngressGateways[0].ServiceURL
	}
	if len(cfg.LocalGateways) > 0 && !ia.IsPublic() {
		return cfg.LocalGateways[0].ServiceURL
	}
	return ""
}

// getLBStatus get LB Status
func getLBStatus(gatewayServiceURL string) []v1alpha1.LoadBalancerIngressStatus {
	// The ClusterIngress isn't load-balanced by any particular
	// Service, but through a Service mesh.
	if gatewayServiceURL == "" {
		return []v1alpha1.LoadBalancerIngressStatus{
			{MeshOnly: true},
		}
	}
	return []v1alpha1.LoadBalancerIngressStatus{
		{DomainInternal: gatewayServiceURL},
	}
}

func enablesAutoTLS(ctx context.Context) bool {
	return config.FromContext(ctx).Network.AutoTLS
}
