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
	"reflect"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"

	"github.com/knative/pkg/apis/istio/v1alpha3"
	istioinformers "github.com/knative/pkg/client/informers/externalversions/istio/v1alpha3"
	istiolisters "github.com/knative/pkg/client/listers/istio/v1alpha3"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	informers "github.com/knative/serving/pkg/client/informers/externalversions/networking/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/clusteringress/resources"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/clusteringress/resources/names"
)

const controllerAgentName = "clusteringress-controller"

// Reconciler implements controller.Reconciler for ClusterIngress resources.
type Reconciler struct {
	*reconciler.Base

	// listers index properties about resources
	clusterIngressLister listers.ClusterIngressLister
	virtualServiceLister istiolisters.VirtualServiceLister
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events.
func NewController(
	opt reconciler.Options,
	clusterIngressInformer informers.ClusterIngressInformer,
	virtualServiceInformer istioinformers.VirtualServiceInformer,
) *controller.Impl {

	c := &Reconciler{
		Base:                 reconciler.NewBase(opt, controllerAgentName),
		clusterIngressLister: clusterIngressInformer.Lister(),
		virtualServiceLister: virtualServiceInformer.Lister(),
	}
	impl := controller.NewImpl(c, c.Logger, "ClusterIngresses", reconciler.MustNewStatsReporter("ClusterIngress", c.Logger))

	c.Logger.Info("Setting up event handlers")
	clusterIngressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    impl.Enqueue,
		UpdateFunc: controller.PassNew(impl.Enqueue),
		DeleteFunc: impl.Enqueue,
	})

	virtualServiceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    impl.EnqueueLabelOf("", networking.IngressLabelKey),
		UpdateFunc: controller.PassNew(impl.EnqueueLabelOf("", networking.IngressLabelKey)),
		DeleteFunc: impl.EnqueueLabelOf("", networking.IngressLabelKey),
	})

	return impl
}

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
	err = c.reconcile(ctx, ci)
	if equality.Semantic.DeepEqual(original.Status, ci.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if _, err := c.updateStatus(ctx, ci); err != nil {
		logger.Warn("Failed to update clusterIngress status", zap.Error(err))
		c.Recorder.Eventf(ci, corev1.EventTypeWarning, "UpdateFailed",
			"Failed to update status for ClusterIngress %q: %v", ci.Name, err)
		return err
	}
	return err
}

// Update the Status of the ClusterIngress.  Caller is responsible for checking
// for semantic differences before calling.
func (c *Reconciler) updateStatus(ctx context.Context, desired *v1alpha1.ClusterIngress) (*v1alpha1.ClusterIngress, error) {
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
	// TODO: for CRD there's no updatestatus, so use normal update.
	updated, err := c.ServingClientSet.NetworkingV1alpha1().ClusterIngresses().Update(existing)
	if err != nil {
		return nil, err
	}

	c.Recorder.Eventf(desired, corev1.EventTypeNormal, "Updated", "Updated status for clusterIngress %q", desired.Name)
	return updated, nil
}

func (c *Reconciler) reconcile(ctx context.Context, ci *v1alpha1.ClusterIngress) error {
	logger := logging.FromContext(ctx)
	ci.Status.InitializeConditions()
	vs := resources.MakeVirtualService(ci)

	logger.Infof("Reconciling clusterIngress :%v", ci)
	logger.Info("Creating/Updating VirtualService")
	if err := c.reconcileVirtualService(ctx, ci, vs); err != nil {
		// TODO(lichuqiang): should we explicitly mark the ingress as unready
		// when error reconciling VirtualService?
		return err
	}
	// As underlying network programming (VirtualService now) is stateless,
	// here we simply mark the ingress as ready if the VirtualService
	// is successfully synced.
	ci.Status.MarkNetworkConfigured()
	ci.Status.MarkLoadBalancerReady([]v1alpha1.LoadBalancerIngressStatus{
		{DomainInternal: names.K8sGatewayServiceFullname},
	})
	logger.Info("ClusterIngress successfully synced")
	return nil
}

func (c *Reconciler) reconcileVirtualService(ctx context.Context, ci *v1alpha1.ClusterIngress,
	desired *v1alpha3.VirtualService) error {
	logger := logging.FromContext(ctx)
	ns := desired.Namespace
	name := desired.Name

	vs, err := c.virtualServiceLister.VirtualServices(ns).Get(name)
	if apierrs.IsNotFound(err) {
		vs, err = c.SharedClientSet.NetworkingV1alpha3().VirtualServices(ns).Create(desired)
		if err != nil {
			logger.Error("Failed to create VirtualService", zap.Error(err))
			c.Recorder.Eventf(ci, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create VirtualService %q/%q: %v", ns, name, err)
			return err
		}
		c.Recorder.Eventf(ci, corev1.EventTypeNormal, "Created",
			"Created VirtualService %q", desired.Name)
	} else if err != nil {
		return err
	} else if !equality.Semantic.DeepEqual(vs.Spec, desired.Spec) {
		// Don't modify the informers copy
		existing := vs.DeepCopy()
		existing.Spec = desired.Spec
		_, err = c.SharedClientSet.NetworkingV1alpha3().VirtualServices(ns).Update(existing)
		if err != nil {
			logger.Error("Failed to update VirtualService", zap.Error(err))
			return err
		}
		c.Recorder.Eventf(desired, corev1.EventTypeNormal, "Updated",
			"Updated status for VirtualService %q/%q", ns, name)
	}

	return nil
}
