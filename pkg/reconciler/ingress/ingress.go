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
	"sort"

	istiov1alpha3 "istio.io/api/networking/v1alpha3"
	"istio.io/client-go/pkg/apis/networking/v1alpha3"
	"knative.dev/pkg/logging"
	listers "knative.dev/serving/pkg/client/listers/networking/v1alpha1"

	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/pkg/tracker"
	istiolisters "knative.dev/serving/pkg/client/istio/listers/networking/v1alpha3"

	"knative.dev/serving/pkg/apis/networking"
	"knative.dev/serving/pkg/apis/networking/v1alpha1"
	"knative.dev/serving/pkg/network"
	"knative.dev/serving/pkg/network/status"
	"knative.dev/serving/pkg/reconciler/ingress/config"
	"knative.dev/serving/pkg/reconciler/ingress/resources"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	ingressreconciler "knative.dev/serving/pkg/client/injection/reconciler/networking/v1alpha1/ingress"
	istioclientset "knative.dev/serving/pkg/client/istio/clientset/versioned"
	spkgreconciler "knative.dev/serving/pkg/reconciler"
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
	*spkgreconciler.Base

	istioClientSet       istioclientset.Interface
	virtualServiceLister istiolisters.VirtualServiceLister
	gatewayLister        istiolisters.GatewayLister
	secretLister         corev1listers.SecretLister
	ingressLister        listers.IngressLister

	tracker   tracker.Interface
	finalizer string

	statusManager status.Manager
}

var (
	_ ingressreconciler.Interface          = (*Reconciler)(nil)
	_ ingressreconciler.Finalizer          = (*Reconciler)(nil)
	_ coreaccessor.SecretAccessor          = (*Reconciler)(nil)
	_ istioaccessor.VirtualServiceAccessor = (*Reconciler)(nil)
)

func newReconciledNormal(namespace, name string) pkgreconciler.Event {
	return pkgreconciler.NewEvent(corev1.EventTypeNormal, "IngressTypeReconciled", "IngressType reconciled: \"%s/%s\"", namespace, name)
}

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Ingress resource
// with the current status of the resource.
func (r *Reconciler) ReconcileKind(ctx context.Context, ingress *v1alpha1.Ingress) pkgreconciler.Event {
	logger := logging.FromContext(ctx)

	reconcileErr := r.reconcileIngress(ctx, ingress)
	if reconcileErr != nil {
		logger.Errorf("Failed to reconcile Ingress %s", ingress.Name, reconcileErr)
		ingress.Status.MarkIngressNotReady(notReconciledReason, notReconciledMessage)
		return reconcileErr
	}

	return newReconciledNormal(ingress.Namespace, ingress.Name)
}

func (r *Reconciler) reconcileIngress(ctx context.Context, ing *v1alpha1.Ingress) error {
	logger := logging.FromContext(ctx)

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
			if err := r.reconcileIngressServers(ctx, ing, gw, desired); err != nil {
				return err
			}
		}
	}

	// HTTPProtocol should be effective only when Auto TLS is enabled per its definition.
	// TODO(zhiminx): figure out a better way to handle HTTP behavior.
	// https://github.com/knative/serving/issues/6373
	if config.FromContext(ctx).Network.AutoTLS {
		desiredHTTPServer := resources.MakeHTTPServer(config.FromContext(ctx).Network.HTTPProtocol, []string{"*"})
		for _, gw := range config.FromContext(ctx).Istio.IngressGateways {
			if err := r.reconcileHTTPServer(ctx, ing, gw, desiredHTTPServer); err != nil {
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
		if d.GetAnnotations()[networking.IngressClassAnnotationKey] != network.IstioIngressClassName {
			// We do not create resources that do not have istio ingress class annotation.
			// As a result, obsoleted resources will be cleaned up.
			continue
		}
		if _, err := istioaccessor.ReconcileVirtualService(ctx, ing, d, r); err != nil {
			if kaccessor.IsNotOwned(err) {
				ing.Status.MarkResourceNotOwned("VirtualService", d.Name)
			}
			return err
		}
		kept.Insert(d.Name)
	}

	// Now, remove the extra ones.
	vses, err := r.virtualServiceLister.VirtualServices(ing.GetNamespace()).List(
		labels.SelectorFromSet(labels.Set{networking.IngressLabelKey: ing.GetName()}))
	if err != nil {
		return fmt.Errorf("failed to get VirtualServices: %w", err)
	}

	// Sort the virtual services by their name to get a stable deletion order.
	sort.Slice(vses, func(i, j int) bool {
		return vses[i].Name < vses[j].Name
	})

	for _, vs := range vses {
		n, ns := vs.Name, vs.Namespace
		if kept.Has(n) {
			continue
		}
		if !metav1.IsControlledBy(vs, ing) {
			// We shouldn't remove resources not controlled by us.
			continue
		}
		if err = r.istioClientSet.NetworkingV1alpha3().VirtualServices(ns).Delete(n, &metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("failed to delete VirtualService: %w", err)
		}
	}
	return nil
}

func (r *Reconciler) FinalizeKind(ctx context.Context, ing *v1alpha1.Ingress) pkgreconciler.Event {
	logger := logging.FromContext(ctx)
	istiocfg := config.FromContext(ctx).Istio
	logger.Infof("Cleaning up Gateway Servers for Ingress %s", ing.GetName())
	for _, gws := range [][]config.Gateway{istiocfg.IngressGateways, istiocfg.LocalGateways} {
		for _, gw := range gws {
			if err := r.reconcileIngressServers(ctx, ing, gw, []*istiov1alpha3.Server{}); err != nil {
				return err
			}
		}
	}

	// Update the Ingress to remove the finalizer.
	logger.Info("Removing finalizer")
	ing.SetFinalizers(ing.GetFinalizers()[1:])
	_, err := r.ServingClientSet.NetworkingV1alpha1().Ingresses(ing.GetNamespace()).Update(ing)
	if err != nil {
		logger.Infof("error removing finalizer  %s", err.Error())
	}
	return nil
}

func (r *Reconciler) reconcileIngressServers(ctx context.Context, ing *v1alpha1.Ingress, gw config.Gateway, desired []*istiov1alpha3.Server) error {
	gateway, err := r.gatewayLister.Gateways(gw.Namespace).Get(gw.Name)
	if err != nil {
		// Unlike VirtualService, a default gateway needs to be existent.
		// It should be installed when installing Knative.
		return fmt.Errorf("failed to get Gateway: %w", err)
	}
	existing := resources.GetServers(gateway, ing)
	return r.reconcileGateway(ctx, ing, gateway, existing, desired)
}

func (r *Reconciler) reconcileHTTPServer(ctx context.Context, ing *v1alpha1.Ingress, gw config.Gateway, desiredHTTP *istiov1alpha3.Server) error {
	gateway, err := r.gatewayLister.Gateways(gw.Namespace).Get(gw.Name)
	if err != nil {
		// Unlike VirtualService, a default gateway needs to be existent.
		// It should be installed when installing Knative.
		return fmt.Errorf("failed to get Gateway: %w", err)
	}
	existing := []*istiov1alpha3.Server{}
	if e := resources.GetHTTPServer(gateway); e != nil {
		existing = append(existing, e)
	}
	desired := []*istiov1alpha3.Server{}
	if desiredHTTP != nil {
		desired = append(desired, desiredHTTP)
	}
	return r.reconcileGateway(ctx, ing, gateway, existing, desired)
}

func (r *Reconciler) reconcileGateway(ctx context.Context, ing *v1alpha1.Ingress, gateway *v1alpha3.Gateway, existing []*istiov1alpha3.Server, desired []*istiov1alpha3.Server) error {
	if equality.Semantic.DeepEqual(existing, desired) {
		return nil
	}

	copy := gateway.DeepCopy()

	copy = resources.UpdateGateway(copy, desired, existing)

	if _, err := r.istioClientSet.NetworkingV1alpha3().Gateways(copy.Namespace).Update(copy); err != nil {
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
	return r.istioClientSet
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

func (r *Reconciler) shouldReconcileTLS(ing *v1alpha1.Ingress) bool {
	// We should keep reconciling the Ingress whose TLS has been reconciled before
	// to make sure deleting IngressTLS will clean up the TLS server in the Gateway.
	return (ing.IsPublic() && len(ing.Spec.TLS) > 0)
}
