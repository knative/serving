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
	"fmt"
	"reflect"

	"knative.dev/pkg/apis/istio/v1alpha3"
	gatewayinformer "knative.dev/pkg/client/injection/informers/istio/v1alpha3/gateway"
	virtualserviceinformer "knative.dev/pkg/client/injection/informers/istio/v1alpha3/virtualservice"
	"knative.dev/pkg/logging"
	ingressinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/ingress"
	listers "knative.dev/serving/pkg/client/listers/networking/v1alpha1"

	istiolisters "knative.dev/pkg/client/listers/istio/v1alpha3"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	podinformer "knative.dev/pkg/injection/informers/kubeinformers/corev1/pod"
	secretinformer "knative.dev/pkg/injection/informers/kubeinformers/corev1/secret"
	serviceinformer "knative.dev/pkg/injection/informers/kubeinformers/corev1/service"
	"knative.dev/pkg/tracker"

	"go.uber.org/zap"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/reconciler"
	"knative.dev/serving/pkg/reconciler/ingress/config"
	"knative.dev/serving/pkg/reconciler/ingress/resources"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	sharedclientset "knative.dev/pkg/client/clientset/versioned"
	istioaccessor "knative.dev/serving/pkg/reconciler/accessor/istio"
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

// ingressFinalizer is the name that we put into the resource finalizer list, e.g.
//  metadata:
//    finalizers:
//    - ingresses.networking.internal.knative.dev
var (
	ingressResource  = v1alpha1.Resource("ingresses")
	ingressFinalizer = ingressResource.String()
)

// BaseIngressReconciler is the conmon struct for InjectReconciles
type BaseIngressReconciler struct {
	*reconciler.Base

	// listers index properties about resources
	VirtualServiceLister istiolisters.VirtualServiceLister
	GatewayLister        istiolisters.GatewayLister
	SecretLister         corev1listers.SecretLister
	ServiceLister        corev1listers.ServiceLister
	ConfigStore          reconciler.ConfigStore

	Tracker   tracker.Interface
	Finalizer string

	StatusManager StatusManager
}

var _ istioaccessor.VirtualServiceAccessor = (*BaseIngressReconciler)(nil)

// NewBaseIngressReconciler creates a new BaseIngressReconciler
func NewBaseIngressReconciler(ctx context.Context, agentName, finalizer string, cmw configmap.Watcher) *BaseIngressReconciler {
	virtualServiceInformer := virtualserviceinformer.Get(ctx)
	gatewayInformer := gatewayinformer.Get(ctx)
	secretInformer := secretinformer.Get(ctx)
	serviceInformer := serviceinformer.Get(ctx)

	base := &BaseIngressReconciler{
		Base:                 reconciler.NewBase(ctx, agentName, cmw),
		VirtualServiceLister: virtualServiceInformer.Lister(),
		GatewayLister:        gatewayInformer.Lister(),
		SecretLister:         secretInformer.Lister(),
		ServiceLister:        serviceInformer.Lister(),
		Finalizer:            finalizer,
	}
	return base
}

// newInitializer creates an Ingress Reconciler and returns ReconcilerInitializer
func newInitializer(ctx context.Context, cmw configmap.Watcher) ReconcilerInitializer {
	ingressInformer := ingressinformer.Get(ctx)
	r := &Reconciler{
		BaseIngressReconciler: NewBaseIngressReconciler(ctx, controllerAgentName, ingressFinalizer, cmw),
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
	gatewayInformer := gatewayinformer.Get(ctx)
	podInformer := podinformer.Get(ctx)

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

	gatewayInfomer := gatewayinformer.Get(ctx)
	gatewayInfomer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
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

	r.Logger.Info("Setting up StatusManager")
	resyncIngressOnVirtualServiceReady := func(vs *v1alpha3.VirtualService) {
		// Reconcile when a VirtualService becomes ready
		impl.EnqueueLabelOfNamespaceScopedResource(serving.RouteNamespaceLabelKey, serving.RouteLabelKey)(vs)
	}
	statusProber := NewStatusProber(r.Logger.Named("status-manager"), gatewayInformer.Lister(),
		podInformer.Lister(), network.NewAutoTransport, resyncIngressOnVirtualServiceReady)
	r.StatusManager = statusProber
	statusProber.Start(ctx.Done())

	virtualServiceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		// Cancel probing when a VirtualService is deleted
		DeleteFunc: func(obj interface{}) {
			vs, ok := obj.(*v1alpha3.VirtualService)
			if ok {
				statusProber.Cancel(vs)
			}
		},
	})
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
	return r.BaseIngressReconciler.ReconcileIngress(r.ConfigStore.ToContext(ctx), r, key)
}

// ReconcileIngress retrieves Ingress by key and performs reconciliation
func (r *BaseIngressReconciler) ReconcileIngress(ctx context.Context, ra ReconcilerAccessor, key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	logger := logging.FromContext(ctx)
	ctx = controller.WithEventRecorder(ctx, r.Recorder)

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
	reconcileErr := r.reconcileIngress(ctx, ra, ingress)
	if equality.Semantic.DeepEqual(original.GetStatus(), ingress.GetStatus()) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else {
		if _, err = r.updateStatus(ra, ingress); err != nil {
			logger.Warnw("Failed to update Ingress status", zap.Error(err))
			r.Recorder.Eventf(ingress, corev1.EventTypeWarning, "UpdateFailed",
				"Failed to update status for Ingress %q: %v", ingress.GetName(), err)
			return err
		}

		logger.Infof("Updated status for Ingress %q", ingress.GetName())
		r.Recorder.Eventf(ingress, corev1.EventTypeNormal, "Updated",
			"Updated status for Ingress %q", ingress.GetName())
	}
	if reconcileErr != nil {
		r.Recorder.Event(ingress, corev1.EventTypeWarning, "InternalError", reconcileErr.Error())
	}
	return reconcileErr
}

func (r *BaseIngressReconciler) reconcileIngress(ctx context.Context, ra ReconcilerAccessor, ia v1alpha1.IngressAccessor) error {
	logger := logging.FromContext(ctx)
	// We should keep reconcileDeleteion temporarily for those Routes that have been deployed with
	// old reconcilation way in which we add finalizer onto Routes.
	// TODO(zhiminx): delete reconcileDeletion logic.
	if ia.GetDeletionTimestamp() != nil {
		return r.reconcileDeletion(ctx, ra, ia)
	}

	// We may be reading a version of the object that was stored at an older version
	// and may not have had all of the assumed defaults specified.  This won't result
	// in this getting written back to the API Server, but lets downstream logic make
	// assumptions about defaulting.
	ia.SetDefaults(ctx)

	ia.GetStatus().InitializeConditions()
	logger.Infof("Reconciling ingress: %#v", ia)

	gatewayNames := qualifiedGatewayNamesFromContext(ctx)
	if enableReconcileGateway(ctx) && ia.IsPublic() {
		// We used to add Servers of the given Ingress to shared Gateways.
		// Now as we start splitting Gateway, we need to remove them from the shared Gateways.
		for _, gw := range config.FromContext(ctx).Istio.IngressGateways {
			// We set the desired Servers as empty slice, which means we want to remove existing Servers of the
			// given Ingress from the Gateway.
			if err := r.reconcileSharedGateway(ctx, ia, gw, []v1alpha3.Server{}); err != nil {
				return err
			}
		}
		originSecrets, err := resources.GetSecrets(ia, r.SecretLister)
		if err != nil {
			return err
		}
		targetSecrets, err := resources.MakeSecrets(ctx, originSecrets, ia)
		if err != nil {
			return err
		}
		if err := r.reconcileCertSecrets(ctx, ia, targetSecrets); err != nil {
			return err
		}
		ingressGateways, err := resources.MakeIngressGateways(ctx, ia, originSecrets, r.ServiceLister)
		if err != nil {
			return err
		}
		for _, ingressGateway := range ingressGateways {
			if err := r.reconcileIngressGateway(ctx, ia, ingressGateway); err != nil {
				return err
			}
		}
		gatewayNames[v1alpha1.IngressVisibilityExternalIP].Insert(resources.GetQualifiedGatewayNames(ingressGateways)...)
	}

	vses, err := resources.MakeVirtualServices(ia, gatewayNames)
	if err != nil {
		return err
	}

	// Create the VirtualServices.
	logger.Infof("Creating/Updating VirtualServices")
	if err := r.reconcileVirtualServices(ctx, ia, vses); err != nil {
		// TODO(lichuqiang): should we explicitly mark the ingress as unready
		// when error reconciling VirtualService?
		return err
	}

	// Update status
	ia.GetStatus().MarkNetworkConfigured()
	ia.GetStatus().ObservedGeneration = ia.GetGeneration()

	lbReady := true
	for _, vs := range vses {
		ready, err := r.StatusManager.IsReady(vs)
		if err != nil {
			return fmt.Errorf("failed to probe VirtualService %s/%s: %v", vs.Namespace, vs.Name, err)
		}

		// We don't break as soon as one VirtualService is not ready because IsReady
		// need to be called on every VirtualService to trigger polling.
		lbReady = lbReady && ready
	}
	if lbReady {
		lbs := getLBStatus(gatewayServiceURLFromContext(ctx, ia))
		publicLbs := getLBStatus(publicGatewayServiceURLFromContext(ctx))
		privateLbs := getLBStatus(privateGatewayServiceURLFromContext(ctx))

		ia.GetStatus().MarkLoadBalancerReady(lbs, publicLbs, privateLbs)
	} else {
		ia.GetStatus().MarkLoadBalancerPending()
	}

	// TODO(zhiminx): Mark Route status to indicate that Gateway is configured.
	logger.Info("ClusterIngress successfully synced")
	return nil
}

func (r *BaseIngressReconciler) reconcileIngressGateway(ctx context.Context, ia v1alpha1.IngressAccessor, desired *v1alpha3.Gateway) error {
	logger := logging.FromContext(ctx)
	gateway, err := r.GatewayLister.Gateways(desired.Namespace).Get(desired.Name)
	if apierrs.IsNotFound(err) {
		_, err = r.SharedClientSet.NetworkingV1alpha3().Gateways(desired.Namespace).Create(desired)
		if err != nil {
			logger.Errorw("Failed to create Ingress Gateway.", zap.Error(err))
			r.Recorder.Eventf(ia, corev1.EventTypeWarning, "CreationFailed", "Failed to create Gateway %q: %v", desired.Name, err)
			return err
		}
		r.Recorder.Eventf(ia, corev1.EventTypeNormal, "Created", "Created Gateway %q/%q", desired.Namespace, desired.Name)
	} else if err != nil {
		logger.Errorf("Failed to reconcile Ingress: %q failed to Get Gateway: %q", ia.GetName(), desired.Name)
		return err
	} else if !metav1.IsControlledBy(gateway, ia) {
		ia.GetStatus().MarkResourceNotOwned("Gateway", gateway.Name)
		return fmt.Errorf("ingress :%q does not own Gateway: %q", ia.GetName(), gateway.Name)
	} else if !equality.Semantic.DeepEqual(gateway.Spec, desired.Spec) {
		existing := gateway.DeepCopy()
		existing.Spec = desired.Spec
		_, err = r.SharedClientSet.NetworkingV1alpha3().Gateways(existing.Namespace).Update(existing)
		if err != nil {
			logger.Errorw("Failed to update Gateway", zap.Error(err))
			r.Recorder.Eventf(ia, corev1.EventTypeWarning, "UpdateFailed", "Failed to update Gateway %s/%s: %v", desired.Namespace, desired.Name, err)
			return err
		}
		r.Recorder.Eventf(ia, corev1.EventTypeNormal, "Updated", "Updated Gateway %q/%q", existing.Namespace, existing.Name)
	}
	return nil
}

func (r *BaseIngressReconciler) reconcileCertSecrets(ctx context.Context, ia v1alpha1.IngressAccessor, desiredSecrets []*corev1.Secret) error {
	for _, certSecret := range desiredSecrets {
		if err := r.reconcileCertSecret(ctx, ia, certSecret); err != nil {
			return err
		}
	}
	return nil
}

func (r *BaseIngressReconciler) reconcileCertSecret(ctx context.Context, ia v1alpha1.IngressAccessor, desired *corev1.Secret) error {
	// We track the origin and desired secrets so that desired secrets could be synced accordingly when the origin TLS certificate
	// secret is refreshed.
	r.Tracker.Track(resources.SecretRef(desired.Namespace, desired.Name), ia)
	r.Tracker.Track(resources.SecretRef(desired.Labels[networking.OriginSecretNamespaceLabelKey], desired.Labels[networking.OriginSecretNameLabelKey]), ia)

	logger := logging.FromContext(ctx)
	existing, err := r.SecretLister.Secrets(desired.Namespace).Get(desired.Name)
	if apierrs.IsNotFound(err) {
		_, err = r.KubeClientSet.CoreV1().Secrets(desired.Namespace).Create(desired)
		if err != nil {
			logger.Errorw("Failed to create Certificate Secret", zap.Error(err))
			r.Recorder.Eventf(ia, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create Secret %s/%s: %v", desired.Namespace, desired.Name, err)
			return err
		}
		r.Recorder.Eventf(ia, corev1.EventTypeNormal, "Created", "Created Secret %s/%s", desired.Namespace, desired.Name)
	} else if err != nil {
		return err
	} else if !equality.Semantic.DeepEqual(existing.Data, desired.Data) {
		// Don't modify the informers copy
		copy := existing.DeepCopy()
		copy.Data = desired.Data
		_, err = r.KubeClientSet.CoreV1().Secrets(copy.Namespace).Update(copy)
		if err != nil {
			logger.Errorw("Failed to update target secret", zap.Error(err))
			r.Recorder.Eventf(ia, corev1.EventTypeWarning, "UpdateFailed", "Failed to update Secret %s/%s: %v", desired.Namespace, desired.Name, err)
			return err
		}
		r.Recorder.Eventf(ia, corev1.EventTypeNormal, "Updated", "Updated Secret %s/%s", copy.Namespace, copy.Name)
	}
	return nil
}

func (r *BaseIngressReconciler) reconcileVirtualServices(ctx context.Context, ia v1alpha1.IngressAccessor,
	desired []*v1alpha3.VirtualService) error {
	logger := logging.FromContext(ctx)
	// First, create all needed VirtualServices.
	kept := sets.NewString()
	for _, d := range desired {
		if _, err := istioaccessor.ReconcileVirtualService(ctx, ia, d, r); err != nil {
			return err
		}
		kept.Insert(d.Name)
	}
	// Now, remove the extra ones.
	vses, err := r.VirtualServiceLister.VirtualServices(resources.VirtualServiceNamespace(ia)).List(
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
		if err = r.SharedClientSet.NetworkingV1alpha3().VirtualServices(ns).Delete(n, &metav1.DeleteOptions{}); err != nil {
			logger.Errorw("Failed to delete VirtualService", zap.Error(err))
			return err
		}
	}
	return nil
}

func (r *BaseIngressReconciler) reconcileDeletion(ctx context.Context, ra ReconcilerAccessor, ia v1alpha1.IngressAccessor) error {
	logger := logging.FromContext(ctx)

	// If our Finalizer is first, delete the `Servers` from Gateway for this ClusterIngress,
	// and remove the finalizer.
	if len(ia.GetFinalizers()) == 0 || ia.GetFinalizers()[0] != r.Finalizer {
		return nil
	}

	istiocfg := config.FromContext(ctx).Istio
	logger.Infof("Cleaning up Gateway Servers for ClusterIngress %s", ia.GetName())
	for _, gws := range [][]config.Gateway{istiocfg.IngressGateways, istiocfg.LocalGateways} {
		for _, gw := range gws {
			if err := r.reconcileSharedGateway(ctx, ia, gw, []v1alpha3.Server{}); err != nil {
				return err
			}
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
func (r *BaseIngressReconciler) updateStatus(ra ReconcilerAccessor, desired v1alpha1.IngressAccessor) (v1alpha1.IngressAccessor, error) {
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

func (r *BaseIngressReconciler) reconcileSharedGateway(ctx context.Context, ia v1alpha1.IngressAccessor, gw config.Gateway, desired []v1alpha3.Server) error {
	logger := logging.FromContext(ctx)
	gateway, err := r.GatewayLister.Gateways(gw.Namespace).Get(gw.Name)
	if err != nil {
		// Not like VirtualService, A default gateway needs to be existed.
		// It should be installed when installing Knative.
		logger.Errorw("Failed to get Gateway.", zap.Error(err))
		return err
	}

	existing := resources.GetServers(gateway, ia)
	// We should reconcile Gateway if there is any "default" wildcard Server in the Gateway
	// in order to remove those Servers to prevent host name conflict.
	if !equality.Semantic.DeepEqual(existing, desired) || len(resources.GetDefaultServers(gateway)) != 0 {
		copy := gateway.DeepCopy()
		copy = resources.UpdateGateway(copy, desired, existing)
		if _, err := r.SharedClientSet.NetworkingV1alpha3().Gateways(copy.Namespace).Update(copy); err != nil {
			logger.Errorw("Failed to update Gateway", zap.Error(err))
			return err
		}
		r.Recorder.Eventf(ia, corev1.EventTypeNormal, "Updated", "Updated Gateway %q/%q", gateway.Namespace, gateway.Name)
	}
	return nil
}

// GetSharedClient returns the client to access shared resources.
func (r *BaseIngressReconciler) GetSharedClient() sharedclientset.Interface {
	return r.SharedClientSet
}

// GetVirtualServiceLister returns the lister for VirtualService.
func (r *BaseIngressReconciler) GetVirtualServiceLister() istiolisters.VirtualServiceLister {
	return r.VirtualServiceLister
}

// qualifiedGatewayNamesFromContext get gateway names from context
func qualifiedGatewayNamesFromContext(ctx context.Context) map[v1alpha1.IngressVisibility]sets.String {
	publicGateways := sets.NewString()
	for _, gw := range config.FromContext(ctx).Istio.IngressGateways {
		publicGateways.Insert(gw.QualifiedName())
	}

	privateGateways := sets.NewString()
	for _, gw := range config.FromContext(ctx).Istio.LocalGateways {
		privateGateways.Insert(gw.QualifiedName())
	}

	return map[v1alpha1.IngressVisibility]sets.String{
		v1alpha1.IngressVisibilityExternalIP:   publicGateways,
		v1alpha1.IngressVisibilityClusterLocal: privateGateways,
	}
}

// gatewayServiceURLFromContext return an address of a load-balancer
// that the given Ingress is exposed to, or empty string if
// none.
func gatewayServiceURLFromContext(ctx context.Context, ia v1alpha1.IngressAccessor) string {
	if ia.IsPublic() {
		return publicGatewayServiceURLFromContext(ctx)
	}

	return privateGatewayServiceURLFromContext(ctx)
}

func publicGatewayServiceURLFromContext(ctx context.Context) string {
	cfg := config.FromContext(ctx).Istio
	if len(cfg.IngressGateways) > 0 {
		return cfg.IngressGateways[0].ServiceURL
	}

	return ""
}

func privateGatewayServiceURLFromContext(ctx context.Context) string {
	cfg := config.FromContext(ctx).Istio
	if len(cfg.LocalGateways) > 0 {
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

func enableReconcileGateway(ctx context.Context) bool {
	return config.FromContext(ctx).Network.AutoTLS || config.FromContext(ctx).Istio.ReconcileExternalGateway
}
