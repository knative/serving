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

package kpa

import (
	"context"
	"fmt"
	"reflect"

	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/autoscaling"
	pav1alpha1 "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/autoscaler"
	informers "github.com/knative/serving/pkg/client/informers/externalversions/autoscaling/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/autoscaling/kpa/resources"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "kpa-class-podautoscaler-controller"
)

// Deciders is an interface for notifying the presence or absence of KPAs.
type Deciders interface {
	// Get accesses the Decider resource for this key, returning any errors.
	Get(ctx context.Context, namespace, name string) (*autoscaler.Decider, error)

	// Create adds a Decider resource for a given key, returning any errors.
	Create(ctx context.Context, metric *autoscaler.Decider) (*autoscaler.Decider, error)

	// Delete removes the Decider resource for a given key, returning any errors.
	Delete(ctx context.Context, namespace, name string) error

	// Watch registers a function to call when Decider change.
	Watch(watcher func(string))

	// Update update the Decider resource, return the new Decider or any errors.
	Update(ctx context.Context, metric *autoscaler.Decider) (*autoscaler.Decider, error)
}

// KPAScaler knows how to scale the targets of kpa-class PodAutoscalers.
type KPAScaler interface {
	// Scale attempts to scale the given PA's target to the desired scale.
	Scale(ctx context.Context, pa *pav1alpha1.PodAutoscaler, desiredScale int32) (int32, error)
}

// Reconciler tracks PAs and right sizes the ScaleTargetRef based on the
// information from Deciders.
type Reconciler struct {
	*reconciler.Base
	paLister        listers.PodAutoscalerLister
	endpointsLister corev1listers.EndpointsLister
	kpaDeciders     Deciders
	kpaScaler       KPAScaler
	dynConfig       *autoscaler.DynamicConfig
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// NewController creates an autoscaling Controller.
func NewController(
	opts *reconciler.Options,
	paInformer informers.PodAutoscalerInformer,
	endpointsInformer corev1informers.EndpointsInformer,
	kpaDeciders Deciders,
	kpaScaler KPAScaler,
	dynConfig *autoscaler.DynamicConfig,
) *controller.Impl {

	c := &Reconciler{
		Base:            reconciler.NewBase(*opts, controllerAgentName),
		paLister:        paInformer.Lister(),
		endpointsLister: endpointsInformer.Lister(),
		kpaDeciders:     kpaDeciders,
		kpaScaler:       kpaScaler,
		dynConfig:       dynConfig,
	}
	impl := controller.NewImpl(c, c.Logger, "KPA-Class Autoscaling", reconciler.MustNewStatsReporter("KPA-Class Autoscaling", c.Logger))

	c.Logger.Info("Setting up kpa-class event handlers")
	// Handler PodAutoscalers missing the class annotation for backward compatibility.
	onlyKpaClass := reconciler.AnnotationFilterFunc(autoscaling.ClassAnnotationKey, autoscaling.KPA, true)
	paInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: onlyKpaClass,
		Handler:    reconciler.Handler(impl.Enqueue),
	})

	endpointsInformer.Informer().AddEventHandler(
		reconciler.Handler(impl.EnqueueLabelOfNamespaceScopedResource("", autoscaling.KPALabelKey)))

	// Have the Deciders enqueue the PAs whose decisions have changed.
	kpaDeciders.Watch(impl.EnqueueKey)

	return impl
}

// Reconcile right sizes PA ScaleTargetRefs based on the state of decisions in Deciders.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key %s: %v", key, err))
		return nil
	}
	logger := logging.FromContext(ctx)
	logger.Debug("Reconcile kpa-class PodAutoscaler")

	original, err := c.paLister.PodAutoscalers(namespace).Get(name)
	if errors.IsNotFound(err) {
		logger.Debug("PA no longer exists")
		return c.kpaDeciders.Delete(ctx, namespace, name)
	} else if err != nil {
		return err
	}

	if original.Class() != autoscaling.KPA {
		logger.Warn("Ignoring non-kpa-class PA")
		return nil
	}

	// Don't modify the informer's copy.
	pa := original.DeepCopy()

	// Reconcile this copy of the pa and then write back any status
	// updates regardless of whether the reconciliation errored out.
	err = c.reconcile(ctx, pa)
	if equality.Semantic.DeepEqual(original.Status, pa.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if _, err := c.updateStatus(pa); err != nil {
		logger.Warnw("Failed to update kpa status", zap.Error(err))
		c.Recorder.Eventf(pa, corev1.EventTypeWarning, "UpdateFailed",
			"Failed to update status for PA %q: %v", pa.Name, err)
		return err
	}
	if err != nil {
		c.Recorder.Event(pa, corev1.EventTypeWarning, "InternalError", err.Error())
	}
	return err
}

func (c *Reconciler) reconcile(ctx context.Context, pa *pav1alpha1.PodAutoscaler) error {
	logger := logging.FromContext(ctx)

	if pa.GetDeletionTimestamp() != nil {
		return nil
	}

	// We may be reading a version of the object that was stored at an older version
	// and may not have had all of the assumed defaults specified.  This won't result
	// in this getting written back to the API Server, but lets downstream logic make
	// assumptions about defaulting.
	pa.SetDefaults(ctx)

	pa.Status.InitializeConditions()
	logger.Debug("PA exists")

	desiredDecider := resources.MakeDecider(ctx, pa, c.dynConfig.Current())
	decider, err := c.kpaDeciders.Get(ctx, desiredDecider.Namespace, desiredDecider.Name)
	if errors.IsNotFound(err) {
		decider, err = c.kpaDeciders.Create(ctx, desiredDecider)
		if err != nil {
			logger.Errorf("Error creating Metric: %v", err)
			return err
		}
	} else if err != nil {
		logger.Errorf("Error fetching Metric: %v", err)
		return err
	}

	// Ignore status when reconciling
	desiredDecider.Status = decider.Status
	if !equality.Semantic.DeepEqual(desiredDecider, decider) {
		decider, err = c.kpaDeciders.Update(ctx, desiredDecider)
		if err != nil {
			logger.Errorf("Error update Metric: %v", err)
			return err
		}
	}

	// Get the appropriate current scale from the metric, and right size
	// the scaleTargetRef based on it.
	want, err := c.kpaScaler.Scale(ctx, pa, decider.Status.DesiredScale)
	if err != nil {
		logger.Errorf("Error scaling target: %v", err)
		return err
	}

	// Compare the desired and observed resources to determine our situation.
	got := 0

	// Look up the Endpoints resource to determine the available resources.
	endpoints, err := c.endpointsLister.Endpoints(pa.Namespace).Get(pa.Spec.ServiceName)
	if errors.IsNotFound(err) {
		// Treat not found as zero endpoints, it either hasn't been created
		// or it has been torn down.
	} else if err != nil {
		logger.Errorf("Error checking Endpoints %q: %v", pa.Spec.ServiceName, err)
		return err
	} else {
		for _, es := range endpoints.Subsets {
			got += len(es.Addresses)
		}
	}

	logger.Infof("PA got=%v, want=%v", got, want)

	var serviceLabel string
	var configLabel string
	if pa.Labels != nil {
		serviceLabel = pa.Labels[serving.ServiceLabelKey]
		configLabel = pa.Labels[serving.ConfigurationLabelKey]
	}
	reporter, err := autoscaler.NewStatsReporter(pa.Namespace, serviceLabel, configLabel, pa.Name)
	if err != nil {
		return err
	}

	reporter.ReportActualPodCount(int64(got))
	// negative "want" values represent an empty metrics pipeline and thus no specific request is being made
	if want >= 0 {
		reporter.ReportRequestedPodCount(int64(want))
	}

	switch {
	case want == 0:
		pa.Status.MarkInactive("NoTraffic", "The target is not receiving traffic.")

	case got == 0 && want != 0:
		pa.Status.MarkActivating(
			"Queued", "Requests to the target are being buffered as resources are provisioned.")

	case got > 0:
		pa.Status.MarkActive()
	}

	pa.Status.ObservedGeneration = pa.Generation
	return nil
}

func (c *Reconciler) updateStatus(desired *pav1alpha1.PodAutoscaler) (*pav1alpha1.PodAutoscaler, error) {
	pa, err := c.paLister.PodAutoscalers(desired.Namespace).Get(desired.Name)
	if err != nil {
		return nil, err
	}
	// If there's nothing to update, just return.
	if reflect.DeepEqual(pa.Status, desired.Status) {
		return pa, nil
	}
	// Don't modify the informers copy
	existing := pa.DeepCopy()
	existing.Status = desired.Status

	return c.ServingClientSet.AutoscalingV1alpha1().PodAutoscalers(pa.Namespace).UpdateStatus(existing)
}
