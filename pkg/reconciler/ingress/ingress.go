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

	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"knative.dev/pkg/logging"
	listers "knative.dev/serving/pkg/client/listers/networking/v1alpha1"

	"knative.dev/pkg/controller"
	"knative.dev/pkg/tracker"
	istiolisters "knative.dev/serving/pkg/client/istio/listers/networking/v1alpha3"

	"go.uber.org/zap"
	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/network/status"
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
	istioclientset "knative.dev/serving/pkg/client/istio/clientset/versioned"
	kaccessor "knative.dev/serving/pkg/reconciler/accessor"
	coreaccessor "knative.dev/serving/pkg/reconciler/accessor/core"
	istioaccessor "knative.dev/serving/pkg/reconciler/accessor/istio"
)

const (
	virtualServiceNotReconciled = "ReconcileVirtualServiceFailed"
	notReconciledReason         = "ReconcileIngressFailed"
	notReconciledMessage        = "Ingress reconciliation failed"
)

// ingressfinalizer is the name that we put into the resource finalizer list, e.g.
//  metadata:
//    finalizers:
//    - ingresses.networking.internal.knative.dev
var (
	ingressResource  = v1alpha1.Resource("ingresses")
	ingressFinalizer = ingressResource.String()
)

// Reconciler implements the control loop for the Ingress resources.
type Reconciler struct {
	*reconciler.Base

	virtualServiceLister istiolisters.VirtualServiceLister
	gatewayLister        istiolisters.GatewayLister
	secretLister         corev1listers.SecretLister
	ingressLister        listers.IngressLister

	configStore reconciler.ConfigStore
	tracker     tracker.Interface
	finalizer   string

	statusManager status.Manager
}

var (
	_ controller.Reconciler                = (*Reconciler)(nil)
	_ coreaccessor.SecretAccessor          = (*Reconciler)(nil)
	_ istioaccessor.VirtualServiceAccessor = (*Reconciler)(nil)
)

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Ingress resource
// with the current status of the resource.
func (r *Reconciler) Reconcile(ctx context.Context, key string) error {
	logger := logging.FromContext(ctx)
	ctx = r.configStore.ToContext(ctx)
	ctx = controller.WithEventRecorder(ctx, r.Recorder)

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		logger.Errorf("invalid resource key: %s", key)
		return nil
	}

	// Get the Ingress resource with this namespace and name.
	original, err := r.ingressLister.Ingresses(ns).Get(name)
	if apierrs.IsNotFound(err) {
		// The resource may no longer exist, in which case we stop processing.
		logger.Info("Ingress in work queue no longer exists")
		return nil
	} else if err != nil {
		return err
	}
	// Don't modify the informers copy
	ingress := original.DeepCopy()

	// Reconcile this copy of the Ingress and then write back any status
	// updates regardless of whether the reconciliation errored out.
	reconcileErr := r.reconcileIngress(ctx, ingress)
	if reconcileErr != nil {
		r.Recorder.Event(ingress, corev1.EventTypeWarning, "InternalError", reconcileErr.Error())
		ingress.Status.MarkIngressNotReady(notReconciledReason, notReconciledMessage)
	}
	if equality.Semantic.DeepEqual(original.Status, ingress.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else {
		if err = r.updateStatus(original, ingress); err != nil {
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

func (r *Reconciler) reconcileIngress(ctx context.Context, ing *v1alpha1.Ingress) error {
	logger := logging.FromContext(ctx)
	if ing.GetDeletionTimestamp() != nil {
		return r.reconcileDeletion(ctx, ing)
	}

	// We may be reading a version of the object that was stored at an older version
	// and may not have had all of the assumed defaults specified.  This won't result
	// in this getting written back to the API Server, but lets downstream logic make
	// assumptions about defaulting.
	ing.SetDefaults(ctx)

	ing.Status.InitializeConditions()
	logger.Infof("Reconciling ingress: %#v", ing)

	gatewayNames := qualifiedGatewayNamesFromContext(ctx)
	vses, err := resources.MakeVirtualServices(ing, gatewayNames)
	if err != nil {
		return err
	}

	// First, create the VirtualServices.
	logger.Infof("Creating/Updating VirtualServices")
	ing.Status.ObservedGeneration = ing.GetGeneration()
	if err := r.reconcileVirtualServices(ctx, ing, vses); err != nil {
		ing.Status.MarkLoadBalancerFailed(virtualServiceNotReconciled, err.Error())
		return err
	}

	if r.shouldReconcileTLS(ing) {
		// Add the finalizer before adding `Servers` into Gateway so that we can be sure
		// the `Servers` get cleaned up from Gateway.
		if err := r.ensureFinalizer(ing); err != nil {
			return err
		}

		originSecrets, err := resources.GetSecrets(ing, r.secretLister)
		if err != nil {
			return err
		}
		targetSecrets, err := resources.MakeSecrets(ctx, originSecrets, ing)
		if err != nil {
			return err
		}
		if err := r.reconcileCertSecrets(ctx, ing, targetSecrets); err != nil {
			return err
		}

		for _, gw := range config.FromContext(ctx).Istio.IngressGateways {
			ns, err := resources.ServiceNamespaceFromURL(gw.ServiceURL)
			if err != nil {
				return err
			}
			desired, err := resources.MakeTLSServers(ing, ns, originSecrets)
			if err != nil {
				return err
			}
			if err := r.reconcileGateway(ctx, ing, gw, desired); err != nil {
				return err
			}
		}
	}

	// Update status
	ing.Status.MarkNetworkConfigured()

	ready, err := r.statusManager.IsReady(ctx, ing)
	if err != nil {
		return fmt.Errorf("failed to probe Ingress %s/%s: %w", ing.GetNamespace(), ing.GetName(), err)
	}
	if ready {
		lbs := getLBStatus(gatewayServiceURLFromContext(ctx, ing))
		publicLbs := getLBStatus(publicGatewayServiceURLFromContext(ctx))
		privateLbs := getLBStatus(privateGatewayServiceURLFromContext(ctx))
		ing.Status.MarkLoadBalancerReady(lbs, publicLbs, privateLbs)
	} else {
		ing.Status.MarkLoadBalancerNotReady()
	}

	// TODO(zhiminx): Mark Route status to indicate that Gateway is configured.
	logger.Info("Ingress successfully synced")
	return nil
}

func (r *Reconciler) reconcileCertSecrets(ctx context.Context, ing *v1alpha1.Ingress, desiredSecrets []*corev1.Secret) error {
	for _, certSecret := range desiredSecrets {
		// We track the origin and desired secrets so that desired secrets could be synced accordingly when the origin TLS certificate
		// secret is refreshed.
		r.tracker.Track(resources.SecretRef(certSecret.Namespace, certSecret.Name), ing)
		r.tracker.Track(resources.SecretRef(
			certSecret.Labels[networking.OriginSecretNamespaceLabelKey],
			certSecret.Labels[networking.OriginSecretNameLabelKey]), ing)
		if _, err := coreaccessor.ReconcileSecret(ctx, ing, certSecret, r); err != nil {
			if kaccessor.IsNotOwned(err) {
				ing.Status.MarkResourceNotOwned("Secret", certSecret.Name)
			}
			return err
		}
	}
	return nil
}

func (r *Reconciler) reconcileVirtualServices(ctx context.Context, ing *v1alpha1.Ingress,
	desired []*v1alpha3.VirtualService) error {
	// First, create all needed VirtualServices.
	kept := sets.NewString()
	for _, d := range desired {
		if _, err := istioaccessor.ReconcileVirtualService(ctx, ing, d, r); err != nil {
			if kaccessor.IsNotOwned(err) {
				ing.Status.MarkResourceNotOwned("VirtualService", d.Name)
			}
			return err
		}
		kept.Insert(d.Name)
	}

	// Now, remove the extra ones.
	// TODO(https://github.com/knative/serving/issues/6363):  Switch to use networking.IngressLabelKey instead.
	vses, err := r.virtualServiceLister.VirtualServices(resources.VirtualServiceNamespace(ing)).List(
		labels.Set(map[string]string{
			serving.RouteLabelKey:          ing.GetLabels()[serving.RouteLabelKey],
			serving.RouteNamespaceLabelKey: ing.GetLabels()[serving.RouteNamespaceLabelKey]}).AsSelector())
	if err != nil {
		return fmt.Errorf("failed to get VirtualServices: %w", err)
	}
	for _, vs := range vses {
		n, ns := vs.Name, vs.Namespace
		if kept.Has(n) {
			continue
		}
		if !metav1.IsControlledBy(vs, ing) {
			// We shouldn't remove resources not controlled by us.
			continue
		}
		if err = r.IstioClientSet.NetworkingV1alpha3().VirtualServices(ns).Delete(n, &metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("failed to delete VirtualService: %w", err)
		}
	}
	return nil
}

func (r *Reconciler) reconcileDeletion(ctx context.Context, ing *v1alpha1.Ingress) error {
	logger := logging.FromContext(ctx)

	// If our finalizer is first, delete the `Servers` from Gateway for this Ingress,
	// and remove the finalizer.
	if len(ing.GetFinalizers()) == 0 || ing.GetFinalizers()[0] != r.finalizer {
		return nil
	}
	istiocfg := config.FromContext(ctx).Istio
	logger.Infof("Cleaning up Gateway Servers for Ingress %s", ing.GetName())
	for _, gws := range [][]config.Gateway{istiocfg.IngressGateways, istiocfg.LocalGateways} {
		for _, gw := range gws {
			if err := r.reconcileGateway(ctx, ing, gw, []*istiov1alpha3.Server{}); err != nil {
				return err
			}
		}
	}

	// Update the Ingress to remove the finalizer.
	logger.Info("Removing finalizer")
	ing.SetFinalizers(ing.GetFinalizers()[1:])
	_, err := r.ServingClientSet.NetworkingV1alpha1().Ingresses(ing.GetNamespace()).Update(ing)
	return err
}

// Update the Status of the Ingress.  Caller is responsible for checking
// for semantic differences before calling.
func (r *Reconciler) updateStatus(existing *v1alpha1.Ingress, desired *v1alpha1.Ingress) error {
	existing = existing.DeepCopy()
	return reconciler.RetryUpdateConflicts(func(attempts int) (err error) {
		// The first iteration tries to use the informer's state, subsequent attempts fetch the latest state via API.
		if attempts > 0 {
			existing, err = r.ServingClientSet.NetworkingV1alpha1().Ingresses(desired.GetNamespace()).Get(desired.GetName(), metav1.GetOptions{})
			if err != nil {
				return err
			}
		}

		// If there's nothing to update, just return.
		if reflect.DeepEqual(existing.Status, desired.Status) {
			return nil
		}

		existing.Status = desired.Status
		_, err = r.ServingClientSet.NetworkingV1alpha1().Ingresses(existing.GetNamespace()).UpdateStatus(existing)
		return err
	})
}

func (r *Reconciler) ensureFinalizer(ing *v1alpha1.Ingress) error {
	finalizers := sets.NewString(ing.GetFinalizers()...)
	if finalizers.Has(r.finalizer) {
		return nil
	}

	mergePatch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"finalizers":      append(ing.GetFinalizers(), r.finalizer),
			"resourceVersion": ing.GetResourceVersion(),
		},
	}

	patch, err := json.Marshal(mergePatch)
	if err != nil {
		return err
	}

	_, err = r.ServingClientSet.NetworkingV1alpha1().Ingresses(ing.GetNamespace()).Patch(ing.GetName(), types.MergePatchType, patch)
	return err
}

func (r *Reconciler) reconcileGateway(ctx context.Context, ing *v1alpha1.Ingress, gw config.Gateway, desired []*istiov1alpha3.Server) error {
	// TODO(zhiminx): Need to handle the scenario when deleting Ingress. In this scenario,
	// the Gateway servers of the Ingress need also be removed from Gateway.
	gateway, err := r.gatewayLister.Gateways(gw.Namespace).Get(gw.Name)
	if err != nil {
		// Unlike VirtualService, a default gateway needs to be existent.
		// It should be installed when installing Knative.
		return fmt.Errorf("failed to get Gateway: %w", err)
	}

	existing := resources.GetServers(gateway, ing)
	existingHTTPServer := resources.GetHTTPServer(gateway)
	if existingHTTPServer != nil {
		existing = append(existing, existingHTTPServer)
	}

	desiredHTTPServer := resources.MakeHTTPServer(config.FromContext(ctx).Network.HTTPProtocol, []string{"*"})
	if desiredHTTPServer != nil {
		desired = append(desired, desiredHTTPServer)
	}

	if equality.Semantic.DeepEqual(existing, desired) {
		return nil
	}

	copy := gateway.DeepCopy()
	copy = resources.UpdateGateway(copy, desired, existing)
	if _, err := r.IstioClientSet.NetworkingV1alpha3().Gateways(copy.Namespace).Update(copy); err != nil {
		return fmt.Errorf("failed to update Gateway: %w", err)
	}
	r.Recorder.Eventf(ing, corev1.EventTypeNormal, "Updated", "Updated Gateway %s/%s", gateway.Namespace, gateway.Name)
	return nil
}

// GetKubeClient returns the client to access k8s resources.
func (r *Reconciler) GetKubeClient() kubernetes.Interface {
	return r.KubeClientSet
}

// GetSecretLister returns the lister for Secret.
func (r *Reconciler) GetSecretLister() corev1listers.SecretLister {
	return r.secretLister
}

// GetIstioClient returns the client to access Istio resources.
func (r *Reconciler) GetIstioClient() istioclientset.Interface {
	return r.IstioClientSet
}

// GetVirtualServiceLister returns the lister for VirtualService.
func (r *Reconciler) GetVirtualServiceLister() istiolisters.VirtualServiceLister {
	return r.virtualServiceLister
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
func gatewayServiceURLFromContext(ctx context.Context, ing *v1alpha1.Ingress) string {
	if ing.IsPublic() {
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
	// The Ingress isn't load-balanced by any particular
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

func (r *Reconciler) shouldReconcileTLS(ia *v1alpha1.Ingress) bool {
	// We should keep reconciling the Ingress whose TLS has been reconciled before
	// to make sure deleting IngressTLS will clean up the TLS server in the Gateway.
	return (ia.IsPublic() && len(ia.Spec.TLS) > 0) || r.wasTLSReconciled(ia)
}

func (r *Reconciler) wasTLSReconciled(ia *v1alpha1.Ingress) bool {
	return len(ia.GetFinalizers()) != 0 && ia.GetFinalizers()[0] == r.finalizer
}
