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

package autoscaler

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/resources"
	"go.uber.org/zap"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

// Autoscaler stores current state of an instance of an autoscaler
type Autoscaler struct {
	namespace       string
	revision        string
	metricClient    MetricClient
	endpointsLister corev1listers.EndpointsLister
	reporter        StatsReporter

	// State in panic mode. Carries over multiple Scale calls. Guarded
	// by the stateMux.
	stateMux     sync.Mutex
	panicTime    *time.Time
	maxPanicPods int32

	// specMux guards the current DeciderSpec.
	specMux     sync.RWMutex
	deciderSpec DeciderSpec
}

// New creates a new instance of autoscaler
func New(
	namespace string,
	revision string,
	metricClient MetricClient,
	endpointsInformer corev1informers.EndpointsInformer,
	deciderSpec DeciderSpec,
	reporter StatsReporter) (*Autoscaler, error) {
	if endpointsInformer == nil {
		return nil, errors.New("'endpointsEnformer' must not be nil")
	}
	if reporter == nil {
		return nil, errors.New("stats reporter must not be nil")
	}

	// A new instance of autoscaler is created without panic mode.
	reporter.ReportPanic(0)

	return &Autoscaler{
		namespace:       namespace,
		revision:        revision,
		metricClient:    metricClient,
		endpointsLister: endpointsInformer.Lister(),
		deciderSpec:     deciderSpec,
		reporter:        reporter,
	}, nil
}

// Update reconfigures the UniScaler according to the DeciderSpec.
func (a *Autoscaler) Update(deciderSpec DeciderSpec) error {
	a.specMux.Lock()
	defer a.specMux.Unlock()
	a.deciderSpec = deciderSpec
	return nil
}

// Scale calculates the desired scale based on current statistics given the current time.
// desiredPodCount is the calculated pod count the autoscaler would like to set.
// validScale signifies whether the desiredPodCount should be applied or not.
func (a *Autoscaler) Scale(ctx context.Context, now time.Time) (desiredPodCount int32, validScale bool) {
	logger := logging.FromContext(ctx)

	spec := a.currentSpec()

	originalReadyPodsCount, err := resources.FetchReadyAddressCount(a.endpointsLister, a.namespace, spec.ServiceName)
	if err != nil {
		// If the error is NotFound, then presume 0.
		if !apierrors.IsNotFound(err) {
			logger.Errorw("Failed to get Endpoints via K8S Lister", zap.Error(err))
			return 0, false
		}
	}
	// Use 1 if there are zero current pods.
	readyPodsCount := math.Max(1, float64(originalReadyPodsCount))

	metricKey := NewMetricKey(a.namespace, a.revision)
	observedStableConcurrency, observedPanicConcurrency, err := a.metricClient.StableAndPanicConcurrency(metricKey)
	if err != nil {
		if err == ErrNoData {
			logger.Debug("No data to scale on yet")
		} else {
			logger.Errorw("Failed to obtain metrics", zap.Error(err))
		}
		return 0, false
	}

	desiredStablePodCount := int32(math.Min(math.Ceil(observedStableConcurrency/spec.TargetConcurrency), spec.MaxScaleUpRate*readyPodsCount))
	desiredPanicPodCount := int32(math.Min(math.Ceil(observedPanicConcurrency/spec.TargetConcurrency), spec.MaxScaleUpRate*readyPodsCount))

	a.reporter.ReportStableRequestConcurrency(observedStableConcurrency)
	a.reporter.ReportPanicRequestConcurrency(observedPanicConcurrency)
	a.reporter.ReportTargetRequestConcurrency(spec.TargetConcurrency)

	logger.Debugf("STABLE: Observed average %0.3f concurrency, targeting %v.", observedStableConcurrency, spec.TargetConcurrency)
	logger.Debugf("PANIC: Observed average %0.3f concurrency, targeting %v.", observedPanicConcurrency, spec.TargetConcurrency)

	isOverPanicThreshold := observedPanicConcurrency/readyPodsCount >= spec.PanicThreshold

	a.stateMux.Lock()
	defer a.stateMux.Unlock()
	if a.panicTime == nil && isOverPanicThreshold {
		// Begin panicking when we cross the concurrency threshold in the panic window.
		logger.Info("PANICKING")
		a.panicTime = &now
		a.reporter.ReportPanic(1)
	} else if a.panicTime != nil && !isOverPanicThreshold && a.panicTime.Add(spec.MetricSpec.StableWindow).Before(now) {
		// Stop panicking after the surge has made its way into the stable metric.
		logger.Info("Un-panicking.")
		a.panicTime = nil
		a.maxPanicPods = 0
		a.reporter.ReportPanic(0)
	}

	if a.panicTime != nil {
		logger.Debug("Operating in panic mode.")
		// We do not scale down while in panic mode. Only increases will be applied.
		if desiredPanicPodCount > a.maxPanicPods {
			logger.Infof("Increasing pods from %v to %v.", originalReadyPodsCount, desiredPanicPodCount)
			a.panicTime = &now
			a.maxPanicPods = desiredPanicPodCount
		}
		desiredPodCount = a.maxPanicPods
	} else {
		logger.Debug("Operating in stable mode.")
		desiredPodCount = desiredStablePodCount
	}

	a.reporter.ReportDesiredPodCount(int64(desiredPodCount))
	return desiredPodCount, true
}

func (a *Autoscaler) currentSpec() DeciderSpec {
	a.specMux.RLock()
	defer a.specMux.RUnlock()
	return a.deciderSpec
}
