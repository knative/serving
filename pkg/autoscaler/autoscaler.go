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
	"strings"
	"sync"
	"time"

	"github.com/knative/pkg/logging"
	"go.uber.org/zap"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

const (
	// ActivatorPodName defines the pod name of the activator
	// as defined in the metrics it sends.
	ActivatorPodName string = "activator"

	approximateZero = 1e-8
)

// Stat defines a single measurement at a point in time
type Stat struct {
	// The time the data point was collected on the pod.
	Time *time.Time

	// The unique identity of this pod.  Used to count how many pods
	// are contributing to the metrics.
	PodName string

	// Average number of requests currently being handled by this pod.
	AverageConcurrentRequests float64

	// Number of requests received since last Stat (approximately QPS).
	RequestCount int32

	// Lameduck indicates this Pod has received a shutdown signal.
	// Deprecated and no longer used by newly created Pods.
	LameDuck bool

	// Average number of requests currently being handled by all ready pods of a
	// revision.
	AverageRevConcurrency float64
}

// StatMessage wraps a Stat with identifying information so it can be routed
// to the correct receiver.
type StatMessage struct {
	Key  string
	Stat Stat
}

type statKey struct {
	podName string
	time    time.Time
}

// Creates a new totalAggregation
func newTotalAggregation(window time.Duration) *totalAggregation {
	return &totalAggregation{
		window:              window,
		perPodAggregations:  make(map[string]*perPodAggregation),
		activatorsContained: make(map[string]struct{}),
	}
}

// Holds an aggregation across all pods
type totalAggregation struct {
	window              time.Duration
	perPodAggregations  map[string]*perPodAggregation
	probeCount          int32
	activatorsContained map[string]struct{}
}

// Aggregates a given stat to the correct pod-aggregation
func (agg *totalAggregation) aggregate(stat Stat) {
	current, exists := agg.perPodAggregations[stat.PodName]
	if !exists {
		current = &perPodAggregation{
			window:      agg.window,
			isActivator: isActivator(stat.PodName),
		}
		agg.perPodAggregations[stat.PodName] = current
	}
	current.aggregate(stat)
	if current.isActivator {
		agg.activatorsContained[stat.PodName] = struct{}{}
	}
	agg.probeCount++
}

// The observed concurrency of a revision and the observed concurrency per pod.
// Ignores activator sent metrics if its not the only pod reporting stats.
func (agg *totalAggregation) observedConcurrency(now time.Time) (float64, float64) {
	accumulatedPodConcurrency := float64(0)
	accumulatedRevConcurrency := float64(0)
	activatorConcurrency := float64(0)
	samplePodCount := 0
	for _, perPod := range agg.perPodAggregations {
		if perPod.isActivator {
			activatorConcurrency += perPod.averagePodConcurrency(now)
		} else {
			accumulatedPodConcurrency += perPod.averagePodConcurrency(now)
			accumulatedRevConcurrency += perPod.averageRevConcurrency(now)
			samplePodCount++
		}
	}
	if samplePodCount != 0 {
		accumulatedPodConcurrency = accumulatedPodConcurrency / float64(samplePodCount)
		accumulatedRevConcurrency = accumulatedRevConcurrency / float64(samplePodCount)
	}

	if accumulatedPodConcurrency < approximateZero {
		// Activator is the only pod reporting stats.
		return activatorConcurrency, activatorConcurrency
	}
	return accumulatedRevConcurrency, accumulatedPodConcurrency
}

// Holds an aggregation per pod
type perPodAggregation struct {
	accumulatedPodConcurrency float64
	accumulatedRevConcurrency float64
	probeCount                int32
	window                    time.Duration
	latestStatTime            *time.Time
	isActivator               bool
}

// Aggregates the given concurrency
func (agg *perPodAggregation) aggregate(stat Stat) {
	agg.accumulatedPodConcurrency += stat.AverageConcurrentRequests
	agg.accumulatedRevConcurrency += stat.AverageRevConcurrency
	agg.probeCount++
	if agg.latestStatTime == nil || agg.latestStatTime.Before(*stat.Time) {
		agg.latestStatTime = stat.Time
	}
}

// Calculates the average concurrency on pod level over all values given.
func (agg *perPodAggregation) averagePodConcurrency(now time.Time) float64 {
	if agg.probeCount == 0 {
		return 0.0
	}
	return agg.accumulatedPodConcurrency / float64(agg.probeCount)
}

// Calculates the average concurrency on revision level over all values given.
func (agg *perPodAggregation) averageRevConcurrency(now time.Time) float64 {
	if agg.probeCount == 0 {
		return 0.0
	}
	return agg.accumulatedRevConcurrency / float64(agg.probeCount)
}

// Autoscaler stores current state of an instance of an autoscaler
type Autoscaler struct {
	*DynamicConfig
	key             string
	namespace       string
	revisionService string
	endpointsLister corev1listers.EndpointsLister
	panicking       bool
	panicTime       *time.Time
	maxPanicPods    float64
	reporter        StatsReporter

	// targetMutex guards the elements in the block below.
	targetMutex sync.RWMutex
	target      float64

	// statsMutex guards the elements in the block below.
	statsMutex sync.Mutex
	stats      map[statKey]Stat
}

// New creates a new instance of autoscaler
func New(
	dynamicConfig *DynamicConfig,
	namespace string,
	revisionService string,
	endpointsInformer corev1informers.EndpointsInformer,
	target float64,
	reporter StatsReporter) (*Autoscaler, error) {
	if endpointsInformer == nil {
		return nil, errors.New("Empty interface of EndpointsInformer")
	}
	return &Autoscaler{
		DynamicConfig:   dynamicConfig,
		namespace:       namespace,
		revisionService: revisionService,
		endpointsLister: endpointsInformer.Lister(),
		target:          target,
		stats:           make(map[statKey]Stat),
		reporter:        reporter,
	}, nil
}

// Update reconfigures the UniScaler according to the MetricSpec.
func (a *Autoscaler) Update(spec MetricSpec) error {
	a.targetMutex.Lock()
	defer a.targetMutex.Unlock()
	a.target = spec.TargetConcurrency
	return nil
}

// Record a data point.
func (a *Autoscaler) Record(ctx context.Context, stat Stat) {
	if stat.Time == nil {
		logger := logging.FromContext(ctx)
		logger.Errorf("Missing time from stat: %+v", stat)
		return
	}

	a.statsMutex.Lock()
	defer a.statsMutex.Unlock()

	key := statKey{
		podName: stat.PodName,
		time:    *stat.Time,
	}
	a.stats[key] = stat
}

// Scale calculates the desired scale based on current statistics given the current time.
func (a *Autoscaler) Scale(ctx context.Context, now time.Time) (int32, bool) {
	logger := logging.FromContext(ctx)

	readyPods, err := a.readyPods()
	if err != nil {
		logger.Errorw("Failed to get Endpoints via K8S Lister", zap.Error(err))
		return 0, false
	}

	config := a.Current()

	stableData, panicData, lastStat := a.aggregateData(now, config.StableWindow, config.PanicWindow, logger)
	// Do nothing when we have no data.
	if stableData.probeCount < 1 {
		logger.Debug("No data to scale on.")
		return 0, false
	}

	observedStableConcurrency, observedStableConcurrencyPerPod := stableData.observedConcurrency(now)
	observedPanicConcurrency, observedPanicConcurrencyPerPod := panicData.observedConcurrency(now)

	// Log system totals
	totalCurrentQPS := int32(0)
	totalCurrentConcurrency := float64(0)
	for _, stat := range lastStat {
		totalCurrentQPS = totalCurrentQPS + stat.RequestCount
		totalCurrentConcurrency = totalCurrentConcurrency + stat.AverageConcurrentRequests
	}
	logger.Debugf("Current QPS: %v  Current concurrent clients: %v", totalCurrentQPS, totalCurrentConcurrency)

	target := a.targetConcurrency()
	// Desired pod count is observed concurrency of revision over desired (stable) concurrency per pod.
	// The scaling up rate limited to within MaxScaleUpRate.
	desiredStablePodCount := a.podCountLimited(observedStableConcurrency/target, readyPods)
	desiredPanicPodCount := a.podCountLimited(observedPanicConcurrency/target, readyPods)

	a.reporter.ReportStableRequestConcurrency(observedStableConcurrencyPerPod)
	a.reporter.ReportPanicRequestConcurrency(observedPanicConcurrencyPerPod)
	a.reporter.ReportTargetRequestConcurrency(target)

	logger.Debugf("STABLE: Observed average %0.3f concurrency over %v seconds over %v samples.",
		observedStableConcurrencyPerPod, config.StableWindow, stableData.probeCount)
	logger.Debugf("PANIC: Observed average %0.3f concurrency over %v seconds over %v samples.",
		observedPanicConcurrencyPerPod, config.PanicWindow, panicData.probeCount)

	// Stop panicking after the surge has made its way into the stable metric.
	if a.panicking && a.panicTime.Add(config.StableWindow).Before(now) {
		logger.Info("Un-panicking.")
		a.panicking = false
		a.panicTime = nil
		a.maxPanicPods = 0
	}

	// Begin panicking when we cross the 6 second concurrency threshold.
	if !a.panicking && panicData.probeCount > 0 && observedPanicConcurrencyPerPod >= (target*2) {
		logger.Info("PANICKING")
		a.panicking = true
		a.panicTime = &now
	}

	var desiredPodCount int32

	if a.panicking {
		logger.Debug("Operating in panic mode.")
		a.reporter.ReportPanic(1)
		if desiredPanicPodCount > a.maxPanicPods {
			logger.Infof("Increasing pods from %v to %v.", readyPods, int(desiredPanicPodCount))
			a.panicTime = &now
			a.maxPanicPods = desiredPanicPodCount
		}
		desiredPodCount = int32(math.Ceil(a.maxPanicPods))
	} else {
		logger.Debug("Operating in stable mode.")
		a.reporter.ReportPanic(0)
		desiredPodCount = int32(math.Ceil(desiredStablePodCount))
	}

	a.reporter.ReportDesiredPodCount(int64(desiredPodCount))
	logger.Infof("desiredPodCount=%v observedPanicConcurrencyPerPod=%0.3f observedStableConcurrencyPerPod=%0.3f", desiredPodCount, observedPanicConcurrencyPerPod, observedStableConcurrencyPerPod)
	return desiredPodCount, true
}

func (a *Autoscaler) aggregateData(
	now time.Time, stableWindow, panicWindow time.Duration, logger *zap.SugaredLogger) (*totalAggregation, *totalAggregation, map[string]Stat) {
	a.statsMutex.Lock()
	defer a.statsMutex.Unlock()

	// 60 second window
	stableData := newTotalAggregation(stableWindow)

	// 6 second window
	panicData := newTotalAggregation(panicWindow)

	// Last stat per Pod
	lastStat := make(map[string]Stat)

	// Accumulate stats into their respective buckets
	for key, stat := range a.stats {
		instant := key.time
		if instant.Add(panicWindow).After(now) {
			panicData.aggregate(stat)
		}
		if instant.Add(stableWindow).After(now) {
			stableData.aggregate(stat)

			// If there's no last stat for this pod, set it
			if _, ok := lastStat[stat.PodName]; !ok {
				lastStat[stat.PodName] = stat
			} else if lastStat[stat.PodName].Time.Before(*stat.Time) {
				// If the current last stat is older than the new one, override
				lastStat[stat.PodName] = stat
			}
		} else {
			// Drop metrics after 60 seconds
			delete(a.stats, key)
		}
	}
	return stableData, panicData, lastStat
}

func (a *Autoscaler) targetConcurrency() float64 {
	a.targetMutex.RLock()
	defer a.targetMutex.RUnlock()
	return a.target
}

func (a *Autoscaler) podCountLimited(desiredPodCount, currentPodCount float64) float64 {
	return math.Min(desiredPodCount, a.Current().MaxScaleUpRate*currentPodCount)
}

// readyPods returns the ready IP count in the K8S Endpoints object for a Revision
// via K8S Informer. This is same as ready Pod count.
func (a *Autoscaler) readyPods() (float64, error) {
	readyPods := 0
	endpoints, err := a.endpointsLister.Endpoints(a.namespace).Get(a.revisionService)
	if apierrors.IsNotFound(err) {
		// Treat not found as zero endpoints, it either hasn't been created
		// or it has been torn down.
	} else if err != nil {
		return 0, err
	} else {
		for _, es := range endpoints.Subsets {
			readyPods += len(es.Addresses)
		}
	}

	// Use 1 as minimum for multiplication and division.
	return math.Max(1, float64(readyPods)), nil
}

func isActivator(podName string) bool {
	// TODO(#2282): This can cause naming collisions.
	return strings.HasPrefix(podName, ActivatorPodName)
}

func divide(a, b float64) float64 {
	if math.Abs(b) < approximateZero {
		return 0
	}
	return a / b
}
