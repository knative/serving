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
	"github.com/knative/serving/pkg/autoscaler/aggregation"
	"github.com/knative/serving/pkg/resources"
	"go.uber.org/zap"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

const (
	// bucketSize is the size of the buckets of stats we create.
	bucketSize time.Duration = 2 * time.Second
)

// Stat defines a single measurement at a point in time
type Stat struct {
	// The time the data point was received by autoscaler.
	Time *time.Time

	// The unique identity of this pod.  Used to count how many pods
	// are contributing to the metrics.
	PodName string

	// Average number of requests currently being handled by this pod.
	AverageConcurrentRequests float64

	// Part of AverageConcurrentRequests, for requests going through a proxy.
	AverageProxiedConcurrentRequests float64

	// Number of requests received since last Stat (approximately QPS).
	RequestCount int32

	// Part of RequestCount, for requests going through a proxy.
	ProxiedRequestCount int32
}

// StatMessage wraps a Stat with identifying information so it can be routed
// to the correct receiver.
type StatMessage struct {
	Key  string
	Stat Stat
}

// Autoscaler stores current state of an instance of an autoscaler
type Autoscaler struct {
	namespace       string
	revisionService string
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

	buckets *aggregation.TimedFloat64Buckets
}

// New creates a new instance of autoscaler
func New(
	namespace string,
	revisionService string,
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
		revisionService: revisionService,
		endpointsLister: endpointsInformer.Lister(),
		deciderSpec:     deciderSpec,
		buckets:         aggregation.NewTimedFloat64Buckets(bucketSize),
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

// Record a data point.
func (a *Autoscaler) Record(ctx context.Context, stat Stat) {
	if stat.Time == nil {
		logger := logging.FromContext(ctx)
		logger.Errorf("Missing time from stat: %+v", stat)
		return
	}

	// Proxied requests have been counted at the activator. Subtract
	// AverageProxiedConcurrentRequests to avoid double counting.
	a.buckets.Record(*stat.Time, stat.PodName, stat.AverageConcurrentRequests-stat.AverageProxiedConcurrentRequests)
}

// Scale calculates the desired scale based on current statistics given the current time.
func (a *Autoscaler) Scale(ctx context.Context, now time.Time) (int32, bool) {
	logger := logging.FromContext(ctx)

	spec := a.currentSpec()

	originalReadyPodsCount, err := resources.FetchReadyAddressCount(a.endpointsLister, a.namespace, a.revisionService)
	if err != nil {
		// If the error is NotFound, then presume 0.
		if !apierrors.IsNotFound(err) {
			logger.Errorw("Failed to get Endpoints via K8S Lister", zap.Error(err))
			return 0, false
		}
	}
	// Use 1 if there are zero current pods.
	readyPodsCount := math.Max(1, float64(originalReadyPodsCount))

	// Remove outdated data.
	a.buckets.RemoveOlderThan(now.Add(-spec.MetricSpec.StableWindow))
	if a.buckets.IsEmpty() {
		logger.Debug("No data to scale on.")
		return 0, false
	}

	// Compute data to scale on.
	panicAverage := aggregation.Average{}
	stableAverage := aggregation.Average{}
	a.buckets.ForEachBucket(
		aggregation.YoungerThan(now.Add(-spec.MetricSpec.PanicWindow), panicAverage.Accumulate),
		stableAverage.Accumulate, // No need to add a YoungerThan condition as we already deleted all outdated stats above.
	)
	observedStableConcurrency := stableAverage.Value()
	observedPanicConcurrency := panicAverage.Value()

	// Desired pod count is observed concurrency of the revision over desired (stable) concurrency per pod.
	// The scaling up rate is limited to the MaxScaleUpRate.
	desiredStablePodCount := podCountLimited(math.Ceil(observedStableConcurrency/spec.TargetConcurrency), readyPodsCount, spec.MaxScaleUpRate)
	desiredPanicPodCount := podCountLimited(math.Ceil(observedPanicConcurrency/spec.TargetConcurrency), readyPodsCount, spec.MaxScaleUpRate)

	a.reporter.ReportStableRequestConcurrency(observedStableConcurrency)
	a.reporter.ReportPanicRequestConcurrency(observedPanicConcurrency)
	a.reporter.ReportTargetRequestConcurrency(spec.TargetConcurrency)

	logger.Debugf("STABLE: Observed average %0.3f concurrency over %v seconds.", observedStableConcurrency, spec.MetricSpec.StableWindow)
	logger.Debugf("PANIC: Observed average %0.3f concurrency over %v seconds.", observedPanicConcurrency, spec.MetricSpec.PanicWindow)

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

	var desiredPodCount int32
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

func podCountLimited(desiredPodCount, currentPodCount, maxScaleUpRate float64) int32 {
	return int32(math.Min(desiredPodCount, maxScaleUpRate*currentPodCount))
}
