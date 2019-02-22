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
	"go.uber.org/zap"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

const (
	// ActivatorPodName defines the pod name of the activator
	// as defined in the metrics it sends.
	ActivatorPodName string = "activator"

	// bucketSize is the size of the buckets of stats we create.
	bucketSize time.Duration = 2 * time.Second
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
}

// StatMessage wraps a Stat with identifying information so it can be routed
// to the correct receiver.
type StatMessage struct {
	Key  string
	Stat Stat
}

// StatsBucket keeps all the stats that fall into a defined bucket.
type StatsBucket struct {
	stats map[string][]Stat
}

func (b *StatsBucket) add(stat Stat) {
	podStats, ok := b.stats[stat.PodName]
	if !ok {
		podStats = []Stat{}
	}
	podStats = append(podStats, stat)
	b.stats[stat.PodName] = podStats
}

func (b *StatsBucket) concurrency() float64 {
	var total float64
	for _, podStats := range b.stats {
		var subtotal float64
		for _, stat := range podStats {
			subtotal += stat.AverageConcurrentRequests
		}
		total += subtotal / float64(len(podStats))
	}

	return total
}

// Autoscaler stores current state of an instance of an autoscaler
type Autoscaler struct {
	*DynamicConfig
	key             string
	namespace       string
	revisionService string
	endpointsLister corev1listers.EndpointsLister
	panicTime       *time.Time
	maxPanicPods    int32
	reporter        StatsReporter

	// targetMutex guards the elements in the block below.
	targetMutex sync.RWMutex
	target      float64

	// statsMutex guards the elements in the block below.
	statsMutex sync.Mutex
	bucketed   map[time.Time]*StatsBucket
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
		bucketed:        make(map[time.Time]*StatsBucket),
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

	bucketKey := stat.Time.Truncate(bucketSize)
	bucket, ok := a.bucketed[bucketKey]
	if !ok {
		bucket = &StatsBucket{stats: make(map[string][]Stat)}
		a.bucketed[bucketKey] = bucket
	}
	bucket.add(stat)
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

	observedStableConcurrency, observedPanicConcurrency, ok := a.aggregateData(now, config.StableWindow, config.PanicWindow)
	if !ok {
		logger.Debug("No data to scale on.")
		return 0, false
	}

	// Log system totals
	/*totalCurrentQPS := int32(0)
	totalCurrentConcurrency := float64(0)
	for _, stat := range lastStat {
		totalCurrentQPS = totalCurrentQPS + stat.RequestCount
		totalCurrentConcurrency = totalCurrentConcurrency + stat.AverageConcurrentRequests
	}
	logger.Debugf("Current QPS: %v  Current concurrent clients: %v", totalCurrentQPS, totalCurrentConcurrency)*/

	target := a.targetConcurrency()
	// Desired pod count is observed concurrency of revision over desired (stable) concurrency per pod.
	// The scaling up rate limited to within MaxScaleUpRate.
	desiredStablePodCount := int32(math.Ceil(a.podCountLimited(observedStableConcurrency/target, readyPods)))
	desiredPanicPodCount := int32(math.Ceil(a.podCountLimited(observedPanicConcurrency/target, readyPods)))

	a.reporter.ReportStableRequestConcurrency(observedStableConcurrency)
	a.reporter.ReportPanicRequestConcurrency(observedPanicConcurrency)
	a.reporter.ReportTargetRequestConcurrency(target)

	logger.Debugf("STABLE: Observed average %0.3f concurrency over %v seconds.", observedStableConcurrency, config.StableWindow)
	logger.Debugf("PANIC: Observed average %0.3f concurrency over %v seconds.", observedPanicConcurrency, config.PanicWindow)

	isOverPanicThreshold := observedPanicConcurrency/readyPods >= target*2

	// Stop panicking after the surge has made its way into the stable metric.
	if !isOverPanicThreshold && a.panicTime != nil && a.panicTime.Add(config.StableWindow).Before(now) {
		logger.Info("Un-panicking.")
		a.panicTime = nil
		a.maxPanicPods = 0
	}

	// Begin panicking when we cross the 6 second concurrency threshold.
	if a.panicTime == nil && isOverPanicThreshold {
		logger.Info("PANICKING")
		a.panicTime = &now
	}

	var desiredPodCount int32
	if a.panicTime != nil {
		logger.Debug("Operating in panic mode.")
		a.reporter.ReportPanic(1)
		if desiredPanicPodCount > a.maxPanicPods {
			logger.Infof("Increasing pods from %v to %v.", readyPods, desiredPanicPodCount)
			a.panicTime = &now
			a.maxPanicPods = desiredPanicPodCount
		}
		desiredPodCount = a.maxPanicPods
	} else {
		logger.Debug("Operating in stable mode.")
		a.reporter.ReportPanic(0)
		desiredPodCount = desiredStablePodCount
	}

	a.reporter.ReportDesiredPodCount(int64(desiredPodCount))
	return desiredPodCount, true
}

func (a *Autoscaler) aggregateData(now time.Time, stableWindow, panicWindow time.Duration) (float64, float64, bool) {
	a.statsMutex.Lock()
	defer a.statsMutex.Unlock()

	var stableBuckets float64
	var stableTotal float64

	var panicBuckets float64
	var panicTotal float64

	for bucketTime, bucket := range a.bucketed {
		if !bucketTime.Add(panicWindow).Before(now) {
			panicBuckets++
			panicTotal += bucket.concurrency()
		}
		if !bucketTime.Add(stableWindow).Before(now) {
			stableBuckets++
			stableTotal += bucket.concurrency()
		} else {
			delete(a.bucketed, bucketTime)
		}
	}

	if stableBuckets == 0 {
		return 0, 0, false
	}
	return stableTotal / stableBuckets, panicTotal / panicBuckets, true
}

func (a *Autoscaler) targetConcurrency() float64 {
	a.targetMutex.RLock()
	defer a.targetMutex.RUnlock()
	return a.target
}

func (a *Autoscaler) podCountLimited(desiredPodCount, currentPodCount float64) float64 {
	return math.Min(desiredPodCount, a.Current().MaxScaleUpRate*currentPodCount)
}

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
