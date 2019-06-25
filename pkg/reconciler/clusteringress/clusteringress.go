/*
Copyright 2018 The Knative Authors

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

package clusteringress

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/knative/pkg/tracker"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/knative/pkg/apis/istio/v1alpha3"
	istiolisters "github.com/knative/pkg/client/listers/istio/v1alpha3"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/logging"
	"github.com/knative/pkg/system"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	listers "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/ingress/config"
	"github.com/knative/serving/pkg/reconciler/ingress/resources"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

// clusterIngressFinalizer is the name that we put into the resource finalizer list, e.g.
//  metadata:
//    finalizers:
//    - clusteringresses.networking.internal.knative.dev
var (
	clusterIngressResource  = v1alpha1.Resource("clusteringresses")
	clusterIngressFinalizer = clusterIngressResource.String()
)

// Reconciler implements controller.Reconciler for ClusterIngress resources.
type Reconciler struct {
	*reconciler.Base

	// listers index properties about resources
	clusterIngressLister listers.ClusterIngressLister
	virtualServiceLister istiolisters.VirtualServiceLister
	gatewayLister        istiolisters.GatewayLister
	secretLister         corev1listers.SecretLister
	configStore          reconciler.ConfigStore

	tracker tracker.Interface
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the ClusterIngress resource
// with the current status of the resource.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.Logger.Errorf("invalid resource key: %s", key)
		return nil
	}
	logger := logging.FromContext(ctx)

	ctx = c.configStore.ToContext(ctx)

	// Get the ClusterIngress resource with this name.
	original, err := c.clusterIngressLister.Get(name)
	if apierrs.IsNotFound(err) {
		// The resource may no longer exist, in which case we stop processing.
		logger.Errorf("clusteringress %q in work queue no longer exists", key)
		return nil
	} else if err != nil {
		return err
	}
	// Don't modify the informers copy
	ci := original.DeepCopy()

	// Reconcile this copy of the ClusterIngress and then write back any status
	// updates regardless of whether the reconciliation errored out.
	reconcileErr := c.reconcile(ctx, ci)
	if equality.Semantic.DeepEqual(original.Status, ci.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else {
		if _, err = c.updateStatus(ci); err != nil {
			logger.Warnw("Failed to update ClusterIngress status", zap.Error(err))
			c.Recorder.Eventf(ci, corev1.EventTypeWarning, "UpdateFailed",
				"Failed to update status for ClusterIngress %q: %v", ci.Name, err)
			return err
		}

		logger.Infof("Updated status for ClusterIngress %q", ci.Name)
		c.Recorder.Eventf(ci, corev1.EventTypeNormal, "Updated",
			"Updated status for ClusterIngress %q", ci.Name)
	}
	if reconcileErr != nil {
		c.Recorder.Event(ci, corev1.EventTypeWarning, "InternalError", reconcileErr.Error())
	}
	return reconcileErr
}

// Update the Status of the ClusterIngress.  Caller is responsible for checking
// for semantic differences before calling.
func (c *Reconciler) updateStatus(desired *v1alpha1.ClusterIngress) (*v1alpha1.ClusterIngress, error) {
	ci, err := c.clusterIngressLister.Get(desired.Name)
	if err != nil {
		return nil, err
	}
	// If there's nothing to update, just return.
	if reflect.DeepEqual(ci.Status, desired.Status) {
		return ci, nil
	}
	// Don't modify the informers copy
	existing := ci.DeepCopy()
	existing.Status = desired.Status
	return c.ServingClientSet.NetworkingV1alpha1().ClusterIngresses().UpdateStatus(existing)
}

func (c *Reconciler) reconcile(ctx context.Context, ci *v1alpha1.ClusterIngress) error {
	logger := logging.FromContext(ctx)
	if ci.GetDeletionTimestamp() != nil {
		return c.reconcileDeletion(ctx, ci)
	}

	// We may be reading a version of the object that was stored at an older version
	// and may not have had all of the assumed defaults specified.  This won't result
	// in this getting written back to the API Server, but lets downstream logic make
	// assumptions about defaulting.
	ci.SetDefaults(ctx)

	ci.Status.InitializeConditions()
	logger.Infof("Reconciling clusterIngress: %#v", ci)

	gatewayNames := gatewayNamesFromContext(ctx, ci)
	vses := resources.MakeVirtualServices(ci, gatewayNames)

	// First, create the VirtualServices.
	logger.Infof("Creating/Updating VirtualServices")
	if err := c.reconcileVirtualServices(ctx, ci, vses); err != nil {
		// TODO(lichuqiang): should we explicitly mark the ingress as unready
		// when error reconciling VirtualService?
		return err
	}
	// As underlying network programming (VirtualService now) is stateless,
	// here we simply mark the ingress as ready if the VirtualService
	// is successfully synced.
	ci.Status.MarkNetworkConfigured()
	ci.Status.MarkLoadBalancerReady(getLBStatus(gatewayServiceURLFromContext(ctx, ci)))
	ci.Status.ObservedGeneration = ci.Generation

	if enablesAutoTLS(ctx) {
		if !ci.IsPublic() {
			logger.Infof("ClusterIngress %s is not public. So no need to configure TLS.", ci.Name)
			return nil
		}

		// Add the finalizer before adding `Servers` into Gateway so that we can be sure
		// the `Servers` get cleaned up from Gateway.
		if err := c.ensureFinalizer(ci); err != nil {
			return err
		}

		originSecrets, err := resources.GetSecrets(ci, c.secretLister)
		if err != nil {
			return err
		}
		targetSecrets := resources.MakeSecrets(ctx, originSecrets, ci)
		if err := c.reconcileCertSecrets(ctx, ci, targetSecrets); err != nil {
			return err
		}

		for _, gatewayName := range gatewayNames {
			ns, err := resources.GatewayServiceNamespace(config.FromContext(ctx).Istio.IngressGateways, gatewayName)
			if err != nil {
				return err
			}
			desired, err := resources.MakeServers(ci, ns, originSecrets)
			if err != nil {
				return err
			}
			if err := c.reconcileGateway(ctx, ci, gatewayName, desired); err != nil {
				return err
			}
		}
	}

	// TODO(zhiminx): Mark Route status to indicate that Gateway is configured.
	logger.Info("ClusterIngress successfully synced")
	return nil
}

func enablesAutoTLS(ctx context.Context) bool {
	return config.FromContext(ctx).Network.AutoTLS
}

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

// gatewayServiceURLFromContext return an address of a load-balancer
// that the given ClusterIngress is exposed to, or empty string if
// none.
func gatewayServiceURLFromContext(ctx context.Context, ci *v1alpha1.ClusterIngress) string {
	cfg := config.FromContext(ctx).Istio
	if len(cfg.IngressGateways) > 0 && ci.IsPublic() {
		return cfg.IngressGateways[0].ServiceURL
	}
	if len(cfg.LocalGateways) > 0 && !ci.IsPublic() {
		return cfg.LocalGateways[0].ServiceURL
	}
	return ""
}

func gatewayNamesFromContext(ctx context.Context, ci *v1alpha1.ClusterIngress) []string {
	gateways := []string{}
	if ci.IsPublic() {
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

func (c *Reconciler) reconcileVirtualServices(ctx context.Context, ci *v1alpha1.ClusterIngress,
	desired []*v1alpha3.VirtualService) error {
	logger := logging.FromContext(ctx)
	// First, create all needed VirtualServices.
	kept := sets.NewString()
	for _, d := range desired {
		if err := c.reconcileVirtualService(ctx, ci, d); err != nil {
			return err
		}
		kept.Insert(d.Name)
	}
	// Now, remove the extra ones.
	vses, err := c.virtualServiceLister.VirtualServices(resources.VirtualServiceNamespace(ci)).List(
		labels.Set(map[string]string{
			serving.RouteLabelKey:          ci.Labels[serving.RouteLabelKey],
			serving.RouteNamespaceLabelKey: ci.Labels[serving.RouteNamespaceLabelKey]}).AsSelector())
	if err != nil {
		logger.Errorw("Failed to get VirtualServices", zap.Error(err))
		return err
	}
	for _, vs := range vses {
		n, ns := vs.Name, vs.Namespace
		if kept.Has(n) {
			continue
		}
		if err = c.SharedClientSet.NetworkingV1alpha3().VirtualServices(ns).Delete(n, &metav1.DeleteOptions{}); err != nil {
			logger.Errorw("Failed to delete VirtualService", zap.Error(err))
			return err
		}
	}
	return nil
}

func (c *Reconciler) reconcileVirtualService(ctx context.Context, ci *v1alpha1.ClusterIngress,
	desired *v1alpha3.VirtualService) error {
	logger := logging.FromContext(ctx)
	ns := desired.Namespace
	name := desired.Name

	vs, err := c.virtualServiceLister.VirtualServices(ns).Get(name)
	if apierrs.IsNotFound(err) {
		_, err = c.SharedClientSet.NetworkingV1alpha3().VirtualServices(ns).Create(desired)
		if err != nil {
			logger.Errorw("Failed to create VirtualService", zap.Error(err))
			c.Recorder.Eventf(ci, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create VirtualService %q/%q: %v", ns, name, err)
			return err
		}
		c.Recorder.Eventf(ci, corev1.EventTypeNormal, "Created",
			"Created VirtualService %q", desired.Name)
	} else if err != nil {
		return err
	} else if !metav1.IsControlledBy(vs, ci) {
		// Surface an error in the ClusterIngress's status, and return an error.
		ci.Status.MarkResourceNotOwned("VirtualService", name)
		return fmt.Errorf("ClusterIngress: %q does not own VirtualService: %q", ci.Name, name)
	} else if !equality.Semantic.DeepEqual(vs.Spec, desired.Spec) {
		// Don't modify the informers copy
		existing := vs.DeepCopy()
		existing.Spec = desired.Spec
		_, err = c.SharedClientSet.NetworkingV1alpha3().VirtualServices(ns).Update(existing)
		if err != nil {
			logger.Errorw("Failed to update VirtualService", zap.Error(err))
			return err
		}
		c.Recorder.Eventf(ci, corev1.EventTypeNormal, "Updated",
			"Updated status for VirtualService %q/%q", ns, name)
	}

	return nil
}

func (c *Reconciler) ensureFinalizer(ci *v1alpha1.ClusterIngress) error {
	finalizers := sets.NewString(ci.Finalizers...)
	if finalizers.Has(clusterIngressFinalizer) {
		return nil
	}

	mergePatch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"finalizers":      append(ci.Finalizers, clusterIngressFinalizer),
			"resourceVersion": ci.ResourceVersion,
		},
	}

	patch, err := json.Marshal(mergePatch)
	if err != nil {
		return err
	}

	_, err = c.ServingClientSet.NetworkingV1alpha1().ClusterIngresses().Patch(ci.Name, types.MergePatchType, patch)
	return err
}

func (c *Reconciler) reconcileDeletion(ctx context.Context, ci *v1alpha1.ClusterIngress) error {
	logger := logging.FromContext(ctx)

	// If our Finalizer is first, delete the `Servers` from Gateway for this ClusterIngress,
	// and remove the finalizer.
	if len(ci.Finalizers) == 0 || ci.Finalizers[0] != clusterIngressFinalizer {
		return nil
	}

	gatewayNames := gatewayNamesFromContext(ctx, ci)
	logger.Infof("Cleaning up Gateway Servers for ClusterIngress %s", ci.Name)
	// No desired Servers means deleting all of the existing Servers associated with the CI.
	for _, gatewayName := range gatewayNames {
		if err := c.reconcileGateway(ctx, ci, gatewayName, []v1alpha3.Server{}); err != nil {
			return err
		}
	}

	// Update the ClusterIngress to remove the Finalizer.
	logger.Info("Removing Finalizer")
	ci.Finalizers = ci.Finalizers[1:]
	_, err := c.ServingClientSet.NetworkingV1alpha1().ClusterIngresses().Update(ci)
	return err
}

func (c *Reconciler) reconcileGateway(ctx context.Context, ci *v1alpha1.ClusterIngress, gatewayName string, desired []v1alpha3.Server) error {
	// TODO(zhiminx): Need to handle the scenario when deleting ClusterIngress. In this scenario,
	// the Gateway servers of the ClusterIngress need also be removed from Gateway.
	logger := logging.FromContext(ctx)
	gateway, err := c.gatewayLister.Gateways(system.Namespace()).Get(gatewayName)
	if err != nil {
		// Not like VirtualService, A default gateway needs to be existed.
		// It should be installed when installing Knative.
		logger.Errorw("Failed to get Gateway.", zap.Error(err))
		return err
	}

	existing := resources.GetServers(gateway, ci)
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
	if _, err := c.SharedClientSet.NetworkingV1alpha3().Gateways(copy.Namespace).Update(copy); err != nil {
		logger.Errorw("Failed to update Gateway", zap.Error(err))
		return err
	}
	c.Recorder.Eventf(ci, corev1.EventTypeNormal, "Updated",
		"Updated Gateway %q/%q", gateway.Namespace, gateway.Name)
	return nil
}

func (c *Reconciler) reconcileCertSecrets(ctx context.Context, ci *v1alpha1.ClusterIngress, desiredSecrets []*corev1.Secret) error {
	for _, certSecret := range desiredSecrets {
		if err := c.reconcileCertSecret(ctx, ci, certSecret); err != nil {
			return err
		}
	}
	return nil
}

func (c *Reconciler) reconcileCertSecret(ctx context.Context, ci *v1alpha1.ClusterIngress, desired *corev1.Secret) error {
	// We track the origin and desired secrets so that desired secrets could be synced accordingly when the origin TLS certificate
	// secret is refreshed.
	c.tracker.Track(resources.SecretRef(desired.Namespace, desired.Name), ci)
	c.tracker.Track(resources.SecretRef(desired.Labels[networking.OriginSecretNamespaceLabelKey], desired.Labels[networking.OriginSecretNameLabelKey]), ci)

	logger := logging.FromContext(ctx)
	existing, err := c.secretLister.Secrets(desired.Namespace).Get(desired.Name)
	if apierrs.IsNotFound(err) {
		_, err = c.KubeClientSet.CoreV1().Secrets(desired.Namespace).Create(desired)
		if err != nil {
			logger.Errorw("Failed to create Certificate Secret", zap.Error(err))
			c.Recorder.Eventf(ci, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create Secret %s/%s: %v", desired.Namespace, desired.Name, err)
			return err
		}
		c.Recorder.Eventf(ci, corev1.EventTypeNormal, "Created",
			"Created Secret %s/%s", desired.Namespace, desired.Name)
	} else if err != nil {
		return err
	} else if !equality.Semantic.DeepEqual(existing.Data, desired.Data) {
		// Don't modify the informers copy
		copy := existing.DeepCopy()
		copy.Data = desired.Data
		_, err = c.KubeClientSet.CoreV1().Secrets(copy.Namespace).Update(copy)
		if err != nil {
			logger.Errorw("Failed to update target secret", zap.Error(err))
			c.Recorder.Eventf(ci, corev1.EventTypeWarning, "UpdateFailed",
				"Failed to update Secret %s/%s: %v", desired.Namespace, desired.Name, err)
			return err
		}
		c.Recorder.Eventf(ci, corev1.EventTypeNormal, "Updated",
			"Updated Secret %s/%s", copy.Namespace, copy.Name)
	}
	return nil
}
