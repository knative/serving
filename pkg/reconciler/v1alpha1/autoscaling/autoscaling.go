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

package autoscaling

import (
	"context"
	"fmt"
	"reflect"

	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/logging"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/knative/serving/pkg/apis/autoscaling"
	kpa "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	informers "github.com/knative/serving/pkg/client/informers/externalversions/autoscaling/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
)

const (
	controllerAgentName = "autoscaling-controller"
)

// KPAMetrics is an interface for notifying the presence or absence of KPAs.
type KPAMetrics interface {
	// Get accesses the Metric resource for this key, returning any errors.
	Get(ctx context.Context, key string) (*autoscaler.Metric, error)

	// Create adds a Metric resource for a given key, returning any errors.
	Create(ctx context.Context, kpa *kpa.PodAutoscaler) (*autoscaler.Metric, error)

	// Delete removes the Metric resource for a given key, returning any errors.
	Delete(ctx context.Context, key string) error

	// Watch registers a function to call when Metrics change.
	Watch(watcher func(string))
}

// KPAScaler knows how to scale the targets of KPAs
type KPAScaler interface {
	// Scale attempts to scale the given KPA's target to the desired scale.
	Scale(ctx context.Context, kpa *kpa.PodAutoscaler, desiredScale int32) error
}

// Reconciler tracks KPAs and right sizes the ScaleTargetRef based on the
// information from KPAMetrics.
type Reconciler struct {
	*reconciler.Base

	kpaLister       listers.PodAutoscalerLister
	endpointsLister corev1listers.EndpointsLister

	kpaMetrics KPAMetrics
	kpaScaler  KPAScaler
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// NewController creates an autoscaling Controller.
func NewController(
	opts *reconciler.Options,

	kpaInformer informers.PodAutoscalerInformer,
	endpointsInformer corev1informers.EndpointsInformer,

	kpaMetrics KPAMetrics,
	kpaScaler KPAScaler,
) *controller.Impl {

	c := &Reconciler{
		Base:            reconciler.NewBase(*opts, controllerAgentName),
		kpaLister:       kpaInformer.Lister(),
		endpointsLister: endpointsInformer.Lister(),
		kpaMetrics:      kpaMetrics,
		kpaScaler:       kpaScaler,
	}
	impl := controller.NewImpl(c, c.Logger, "Autoscaling")

	c.Logger.Info("Setting up event handlers")
	kpaInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    impl.Enqueue,
		UpdateFunc: controller.PassNew(impl.Enqueue),
		DeleteFunc: impl.Enqueue,
	})

	endpointsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.EnqueueEndpointsKPA(impl),
		UpdateFunc: controller.PassNew(c.EnqueueEndpointsKPA(impl)),
	})

	// Have the KPAMetrics enqueue the KPAs whose metrics have changed.
	kpaMetrics.Watch(impl.EnqueueKey)

	return impl
}

func (c *Reconciler) EnqueueEndpointsKPA(impl *controller.Impl) func(obj interface{}) {
	return func(obj interface{}) {
		endpoints := obj.(*corev1.Endpoints)
		// Use the label on the Endpoints (from Service) to determine the KPA
		// with which it is associated, and if so queue that KPA.
		if kpaName, ok := endpoints.Labels[autoscaling.KPALabelKey]; ok {
			impl.EnqueueKey(endpoints.Namespace + "/" + kpaName)
		}
	}
}

// Reconcile right sizes KPA ScaleTargetRefs based on the state of metrics in KPAMetrics.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key %s: %v", key, err))
		return nil
	}
	logger := logging.FromContext(ctx)
	logger.Debug("Reconcile KPA")

	original, err := c.kpaLister.PodAutoscalers(namespace).Get(name)
	if errors.IsNotFound(err) {
		logger.Debug("KPA no longer exists")
		return c.kpaMetrics.Delete(ctx, key)
	} else if err != nil {
		return err
	}
	// Don't modify the informer's copy.
	kpa := original.DeepCopy()

	// Reconcile this copy of the kpa and then write back any status
	// updates regardless of whether the reconciliation errored out.
	err = c.reconcile(ctx, key, kpa)
	if equality.Semantic.DeepEqual(original.Status, kpa.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else {
		// logger.Infof("Updating Status (-old, +new): %v", cmp.Diff(original, kpa))
		if _, err := c.updateStatus(kpa); err != nil {
			logger.Warn("Failed to update kpa status", zap.Error(err))
			return err
		}
	}
	return err
}

func (c *Reconciler) reconcile(ctx context.Context, key string, kpa *kpa.PodAutoscaler) error {
	logger := logging.FromContext(ctx)

	kpa.Status.InitializeConditions()
	logger.Debug("KPA exists")

	metric, err := c.kpaMetrics.Get(ctx, key)
	if errors.IsNotFound(err) {
		metric, err = c.kpaMetrics.Create(ctx, kpa)
		if err != nil {
			logger.Errorf("Error creating Metric: %v", err)
			return err
		}
	} else if err != nil {
		logger.Errorf("Error fetching Metric: %v", err)
		return err
	}

	// Get the appropriate current scale from the metric, and right size
	// the scaleTargetRef based on it.
	if err := c.kpaScaler.Scale(ctx, kpa, metric.DesiredScale); err != nil {
		logger.Errorf("Error scaling target: %v", err)
		return err
	}

	// Compare the desired and observed resources to determine our situation.
	got := 0

	// Look up the Endpoints resource to determine the available resources.
	endpoints, err := c.endpointsLister.Endpoints(kpa.Namespace).Get(kpa.Spec.ServiceName)
	if errors.IsNotFound(err) {
		// Treat not found as zero endpoints, it either hasn't been created
		// or it has been torn down.
	} else if err != nil {
		logger.Errorf("Error checking Endpoints %q: %v", kpa.Spec.ServiceName, err)
		return err
	} else {
		for _, es := range endpoints.Subsets {
			got += len(es.Addresses)
		}
	}
	want := metric.DesiredScale
	logger.Infof("KPA got=%v, want=%v", got, want)

	switch {
	case want == 0 || want == -1:
		kpa.Status.MarkInactive("NoTraffic", "The target is not receiving traffic.")

	case got == 0 && want > 0:
		kpa.Status.MarkActivating(
			"Queued", "Requests to the target are being buffered as resources are provisioned.")

	case got > 0:
		kpa.Status.MarkActive()
	}

	return nil
}

func (c *Reconciler) updateStatus(kpa *kpa.PodAutoscaler) (*kpa.PodAutoscaler, error) {
	newKPA, err := c.kpaLister.PodAutoscalers(kpa.Namespace).Get(kpa.Name)
	if err != nil {
		return nil, err
	}
	// Check if there is anything to update.
	if !reflect.DeepEqual(newKPA.Status, kpa.Status) {
		newKPA.Status = kpa.Status

		// TODO: for CRD there's no updatestatus, so use normal update
		return c.ServingClientSet.AutoscalingV1alpha1().PodAutoscalers(kpa.Namespace).Update(newKPA)
		//	return prClient.UpdateStatus(newKPA)
	}
	return kpa, nil
}
