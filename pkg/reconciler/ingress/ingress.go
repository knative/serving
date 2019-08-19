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

	"knative.dev/pkg/apis/istio/v1alpha3"
	gatewayinformer "knative.dev/pkg/client/injection/informers/istio/v1alpha3/gateway"
	virtualserviceinformer "knative.dev/pkg/client/injection/informers/istio/v1alpha3/virtualservice"
	"knative.dev/pkg/logging"
	ingressinformer "knative.dev/serving/pkg/client/injection/informers/networking/v1alpha1/ingress"
	listers "knative.dev/serving/pkg/client/listers/networking/v1alpha1"

	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	podinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"
	secretinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/secret"
	serviceinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/service"
	istiolisters "knative.dev/pkg/client/listers/istio/v1alpha3"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	sharedclientset "knative.dev/pkg/client/clientset/versioned"
	kaccessor "knative.dev/serving/pkg/reconciler/accessor"
	coreaccessor "knative.dev/serving/pkg/reconciler/accessor/core"
	istioaccessor "knative.dev/serving/pkg/reconciler/accessor/istio"
)

const (
	controllerAgentName = "ingress-controller"

	// NotReconciledReason specifies the reason that ingress reconciliation has failed
	NotReconciledReason = "ReconcileIngressFailed"

	// NotReconciledMessage indicates the message that ingress reconciliation has failed
	NotReconciledMessage = "Ingress reconciliation failed"
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
	ConfigStore          reconciler.ConfigStore

	Tracker   tracker.Interface
	Finalizer string

	StatusManager StatusManager
}

var (
	_ coreaccessor.SecretAccessor          = (*BaseIngressReconciler)(nil)
	_ istioaccessor.VirtualServiceAccessor = (*BaseIngressReconciler)(nil)
)

// NewBaseIngressReconciler creates a new BaseIngressReconciler
func NewBaseIngressReconciler(ctx context.Context, agentName, finalizer string, cmw configmap.Watcher) *BaseIngressReconciler {
	virtualServiceInformer := virtualserviceinformer.Get(ctx)
	gatewayInformer := gatewayinformer.Get(ctx)
	secretInformer := secretinformer.Get(ctx)

	base := &BaseIngressReconciler{
		Base:                 reconciler.NewBase(ctx, agentName, cmw),
		VirtualServiceLister: virtualServiceInformer.Lister(),
		GatewayLister:        gatewayInformer.Lister(),
		SecretLister:         secretInformer.Lister(),
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

	SetupSecretTracker(ctx, r, impl)

	r.Logger.Info("Setting up Ingress event handlers")
	ingressInformer := ingressinformer.Get(ctx)
	gatewayInformer := gatewayinformer.Get(ctx)
	endpointsInformer := endpointsinformer.Get(ctx)
	serviceInformer := serviceinformer.Get(ctx)
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

	r.Logger.Info("Setting up ConfigMap receivers")
	configsToResync := []interface{}{
		&config.Istio{},
		&network.Config{},
	}
	resyncIngressesOnConfigChange := configmap.TypeFilter(configsToResync...)(func(string, interface{}) {
		impl.FilteredGlobalResync(myFilterFunc, ingressInformer.Informer())
	})
	configStore := config.NewStore(r.Logger.Named("config-store"), resyncIngressesOnConfigChange)
	configStore.WatchConfigs(cmw)
	r.ConfigStore = configStore

	r.Logger.Info("Setting up StatusManager")
	resyncOnIngressReady := func(ia v1alpha1.IngressAccessor) {
		impl.EnqueueKey(fmt.Sprintf("%s/%s", ia.GetNamespace(), ia.GetName()))
	}
	statusProber := NewStatusProber(
		r.Logger.Named("status-manager"),
		gatewayInformer.Lister(),
		endpointsInformer.Lister(),
		serviceInformer.Lister(),
		resyncOnIngressReady)
	r.StatusManager = statusProber
	statusProber.Start(ctx.Done())

	virtualServiceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		// Cancel probing when a VirtualService is deleted
		DeleteFunc: func(obj interface{}) {
			vs, ok := obj.(*v1alpha3.VirtualService)
			if ok {
				statusProber.CancelVirtualServiceProbing(vs)
			}
		},
	})
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		// Cancel probing when a Pod is deleted
		DeleteFunc: func(obj interface{}) {
			pod, ok := obj.(*corev1.Pod)
			if ok {
				statusProber.CancelPodProbing(pod)
			}
		},
	})
}

// SetupSecretTracker initializes Secret Tracker
func SetupSecretTracker(ctx context.Context, init ReconcilerInitializer, impl *controller.Impl) {

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
	if reconcileErr != nil {
		r.Recorder.Event(ingress, corev1.EventTypeWarning, "InternalError", reconcileErr.Error())
		ingress.GetStatus().MarkIngressNotReady(NotReconciledReason, NotReconciledMessage)
	}
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
	return reconcileErr
}

func (r *BaseIngressReconciler) reconcileIngress(ctx context.Context, ra ReconcilerAccessor, ia v1alpha1.IngressAccessor) error {
	logger := logging.FromContext(ctx)
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
	vses, err := resources.MakeVirtualServices(ia, gatewayNames)
	if err != nil {
		return err
	}

	// First, create the VirtualServices.
	logger.Infof("Creating/Updating VirtualServices")
	ia.GetStatus().ObservedGeneration = ia.GetGeneration()
	if err := r.reconcileVirtualServices(ctx, ia, vses); err != nil {
		return err
	}

	if enableReconcileGateway(ctx) && ia.IsPublic() {
		// Add the finalizer before adding `Servers` into Gateway so that we can be sure
		// the `Servers` get cleaned up from Gateway.
		if err := r.ensureFinalizer(ra, ia); err != nil {
			return err
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

		for _, gw := range config.FromContext(ctx).Istio.IngressGateways {
			ns, err := resources.ServiceNamespaceFromURL(gw.ServiceURL)
			if err != nil {
				return err
			}
			desired, err := resources.MakeTLSServers(ia, ns, originSecrets)
			if err != nil {
				return err
			}
			if err := r.reconcileGateway(ctx, ia, gw, desired); err != nil {
				return err
			}
		}
	}

	// Update status
	ia.GetStatus().MarkNetworkConfigured()

	ready, err := r.StatusManager.IsReady(ia, gatewayNames)
	if err != nil {
		return fmt.Errorf("failed to probe IngressAccessor %s/%s: %v", ia.GetNamespace(), ia.GetName(), err)
	}
	if ready {
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

func (r *BaseIngressReconciler) reconcileCertSecrets(ctx context.Context, ia v1alpha1.IngressAccessor, desiredSecrets []*corev1.Secret) error {
	for _, certSecret := range desiredSecrets {
		// We track the origin and desired secrets so that desired secrets could be synced accordingly when the origin TLS certificate
		// secret is refreshed.
		r.Tracker.Track(resources.SecretRef(certSecret.Namespace, certSecret.Name), ia)
		r.Tracker.Track(resources.SecretRef(
			certSecret.Labels[networking.OriginSecretNamespaceLabelKey],
			certSecret.Labels[networking.OriginSecretNameLabelKey]), ia)
		if _, err := coreaccessor.ReconcileSecret(ctx, ia, certSecret, r); err != nil {
			if kaccessor.IsNotOwned(err) {
				ia.GetStatus().MarkResourceNotOwned("Secret", certSecret.Name)
			}
			return err
		}
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
			if kaccessor.IsNotOwned(err) {
				ia.GetStatus().MarkResourceNotOwned("VirtualService", d.Name)
			}
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
			if err := r.reconcileGateway(ctx, ia, gw, []v1alpha3.Server{}); err != nil {
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

func (r *BaseIngressReconciler) ensureFinalizer(ra ReconcilerAccessor, ia v1alpha1.IngressAccessor) error {
	finalizers := sets.NewString(ia.GetFinalizers()...)
	if finalizers.Has(r.Finalizer) {
		return nil
	}

	mergePatch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"finalizers":      append(ia.GetFinalizers(), r.Finalizer),
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

func (r *BaseIngressReconciler) reconcileGateway(ctx context.Context, ia v1alpha1.IngressAccessor, gw config.Gateway, desired []v1alpha3.Server) error {
	// TODO(zhiminx): Need to handle the scenario when deleting ClusterIngress. In this scenario,
	// the Gateway servers of the ClusterIngress need also be removed from Gateway.
	logger := logging.FromContext(ctx)
	gateway, err := r.GatewayLister.Gateways(gw.Namespace).Get(gw.Name)
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

	desiredHTTPServer := resources.MakeHTTPServer(config.FromContext(ctx).Network.HTTPProtocol, []string{"*"})
	if desiredHTTPServer != nil {
		desired = append(desired, *desiredHTTPServer)
	}

	if equality.Semantic.DeepEqual(existing, desired) {
		return nil
	}

	copy := gateway.DeepCopy()
	copy = resources.UpdateGateway(copy, desired, existing)
	if _, err := r.SharedClientSet.NetworkingV1alpha3().Gateways(copy.Namespace).Update(copy); err != nil {
		logger.Errorw("Failed to update Gateway", zap.Error(err))
		return err
	}
	r.Recorder.Eventf(ia, corev1.EventTypeNormal, "Updated", "Updated Gateway %s/%s", gateway.Namespace, gateway.Name)
	return nil
}

// GetKubeClient returns the client to access k8s resources.
func (r *BaseIngressReconciler) GetKubeClient() kubernetes.Interface {
	return r.KubeClientSet
}

// GetSecretLister returns the lister for Secret.
func (r *BaseIngressReconciler) GetSecretLister() corev1listers.SecretLister {
	return r.SecretLister
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
