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

package scaling

import (
	"context"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/logging"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/autoscaler/metrics"
)

// tickInterval is how often the Autoscaler evaluates the metrics
// and issues a decision.
const tickInterval = 2 * time.Second

// Decider is a resource which observes the request load of a Revision and
// recommends a number of replicas to run.
// +k8s:deepcopy-gen=true
type Decider struct {
	metav1.ObjectMeta
	Spec   DeciderSpec
	Status DeciderStatus
}

// DeciderSpec is the parameters by which the Revision should be scaled.
type DeciderSpec struct {
	MaxScaleUpRate   float64
	MaxScaleDownRate float64
	// The metric used for scaling, i.e. concurrency, rps.
	ScalingMetric string
	// The value of scaling metric per pod that we target to maintain.
	// TargetValue <= TotalValue.
	TargetValue float64
	// The total value of scaling metric that a pod can maintain.
	TotalValue float64
	// The burst capacity that user wants to maintain without queuing at the POD level.
	// Note, that queueing still might happen due to the non-ideal load balancing.
	TargetBurstCapacity float64
	// ActivatorCapacity is the single activator capacity, for subsetting.
	ActivatorCapacity float64
	// PanicThreshold is the threshold at which panic mode is entered. It represents
	// a factor of the currently observed load over the panic window over the ready
	// pods. I.e. if this is 2, panic mode will be entered if the observed metric
	// is twice as high as the current population can handle.
	PanicThreshold float64
	// StableWindow is needed to determine when to exit panic mode.
	StableWindow time.Duration
	// ScaleDownDelay is the time that must pass at reduced concurrency before a
	// scale-down decision is applied.
	ScaleDownDelay time.Duration
	// InitialScale is the calculated initial scale of the revision, taking both
	// revision initial scale and cluster initial scale into account. Revision initial
	// scale overrides cluster initial scale.
	InitialScale int32
	// Reachable describes whether the revision is referenced by any route.
	Reachable bool
}

// DeciderStatus is the current scale recommendation.
type DeciderStatus struct {
	// DesiredScale is the target number of instances that autoscaler
	// this revision needs.
	DesiredScale int32

	// ExcessBurstCapacity is the difference between spare capacity
	// (how much more load the pods in the revision deployment can take before being
	// overloaded) and the configured target burst capacity.
	// If this number is negative: Activator will be threaded in
	// the request path by the PodAutoscaler controller.
	ExcessBurstCapacity int32

	// NumActivators is the computed number of activators
	// necessary to back the revision.
	NumActivators int32
}

// ScaleResult holds the scale result of the UniScaler evaluation cycle.
type ScaleResult struct {
	// DesiredPodCount is the number of pods Autoscaler suggests for the revision.
	DesiredPodCount int32
	// ExcessBurstCapacity is computed headroom of the revision taking into
	// the account target burst capacity.
	ExcessBurstCapacity int32
	// NumActivators is the number of activators required to back this revision.
	NumActivators int32
	// ScaleValid specifies whether this scale result is valid, i.e. whether
	// Autoscaler had all the necessary information to compute a suggestion.
	ScaleValid bool
}

var invalidSR = ScaleResult{
	ScaleValid:    false,
	NumActivators: MinActivators,
}

// UniScaler records statistics for a particular Decider and proposes the scale for the Decider's target based on those statistics.
type UniScaler interface {
	// Scale computes a scaling suggestion for a revision.
	Scale(*zap.SugaredLogger, time.Time) ScaleResult

	// Update reconfigures the UniScaler according to the DeciderSpec.
	Update(*DeciderSpec)
}

// UniScalerFactory creates a UniScaler for a given PA using the given dynamic configuration.
type UniScalerFactory func(*Decider) (UniScaler, error)

// scalerRunner wraps a UniScaler and a channel for implementing shutdown behavior.
type scalerRunner struct {
	scaler UniScaler
	stopCh chan struct{}
	pokeCh chan struct{}
	logger *zap.SugaredLogger

	// mux guards access to decider.
	mux     sync.RWMutex
	decider *Decider
}

func (sr *scalerRunner) latestScale() int32 {
	sr.mux.RLock()
	defer sr.mux.RUnlock()
	return sr.decider.Status.DesiredScale
}

func sameSign(a, b int32) bool {
	return (a&math.MinInt32)^(b&math.MinInt32) == 0
}

// decider returns a thread safe deep copy of the owned decider.
func (sr *scalerRunner) safeDecider() *Decider {
	sr.mux.RLock()
	defer sr.mux.RUnlock()
	return sr.decider.DeepCopy()
}

func (sr *scalerRunner) updateLatestScale(sRes ScaleResult) bool {
	ret := false
	sr.mux.Lock()
	defer sr.mux.Unlock()
	if sr.decider.Status.DesiredScale != sRes.DesiredPodCount {
		sr.decider.Status.DesiredScale = sRes.DesiredPodCount
		ret = true
	}
	if sr.decider.Status.NumActivators != sRes.NumActivators {
		sr.decider.Status.NumActivators = sRes.NumActivators
		ret = true
	}

	// If sign has changed -- then we have to update KPA.
	ret = ret || !sameSign(sr.decider.Status.ExcessBurstCapacity, sRes.ExcessBurstCapacity)

	// Update with the latest calculation anyway.
	sr.decider.Status.ExcessBurstCapacity = sRes.ExcessBurstCapacity
	return ret
}

// MultiScaler maintains a collection of UniScalers.
type MultiScaler struct {
	scalersMutex sync.RWMutex
	scalers      map[types.NamespacedName]*scalerRunner

	scalersStopCh <-chan struct{}

	uniScalerFactory UniScalerFactory

	logger *zap.SugaredLogger

	watcherMutex sync.RWMutex
	watcher      func(types.NamespacedName)

	tickProvider func(time.Duration) *time.Ticker
}

// NewMultiScaler constructs a MultiScaler.
func NewMultiScaler(
	stopCh <-chan struct{},
	uniScalerFactory UniScalerFactory,
	logger *zap.SugaredLogger) *MultiScaler {
	return &MultiScaler{
		scalers:          make(map[types.NamespacedName]*scalerRunner),
		scalersStopCh:    stopCh,
		uniScalerFactory: uniScalerFactory,
		logger:           logger,
		tickProvider:     time.NewTicker,
	}
}

// Get returns the copy of the current Decider.
func (m *MultiScaler) Get(_ context.Context, namespace, name string) (*Decider, error) {
	key := types.NamespacedName{Namespace: namespace, Name: name}
	m.scalersMutex.RLock()
	defer m.scalersMutex.RUnlock()
	scaler, exists := m.scalers[key]
	if !exists {
		// This GroupResource is a lie, but unfortunately this interface requires one.
		return nil, errors.NewNotFound(av1alpha1.Resource("Deciders"), key.String())
	}
	return scaler.safeDecider(), nil
}

// Create instantiates the desired Decider.
func (m *MultiScaler) Create(ctx context.Context, decider *Decider) (*Decider, error) {
	key := types.NamespacedName{Namespace: decider.Namespace, Name: decider.Name}
	m.scalersMutex.Lock()
	defer m.scalersMutex.Unlock()
	scaler, exists := m.scalers[key]
	if !exists {
		var err error
		scaler, err = m.createScaler(ctx, decider)
		if err != nil {
			return nil, err
		}
		m.scalers[key] = scaler
	}
	return scaler.safeDecider(), nil
}

// Update applies the desired DeciderSpec to a currently running Decider.
func (m *MultiScaler) Update(_ context.Context, decider *Decider) (*Decider, error) {
	key := types.NamespacedName{Namespace: decider.Namespace, Name: decider.Name}
	m.scalersMutex.Lock()
	defer m.scalersMutex.Unlock()
	if scaler, exists := m.scalers[key]; exists {
		scaler.mux.Lock()
		defer scaler.mux.Unlock()
		// Make sure we store the copy.
		scaler.decider = decider.DeepCopy()
		scaler.scaler.Update(&decider.Spec)
		return decider, nil
	}
	// This GroupResource is a lie, but unfortunately this interface requires one.
	return nil, errors.NewNotFound(av1alpha1.Resource("Deciders"), key.String())
}

// Delete stops and removes a Decider.
func (m *MultiScaler) Delete(_ context.Context, namespace, name string) {
	key := types.NamespacedName{Namespace: namespace, Name: name}
	m.scalersMutex.Lock()
	defer m.scalersMutex.Unlock()
	if scaler, exists := m.scalers[key]; exists {
		close(scaler.stopCh)
		delete(m.scalers, key)
	}
}

// Watch registers a singleton function to call when DeciderStatus is updated.
func (m *MultiScaler) Watch(fn func(types.NamespacedName)) {
	m.watcherMutex.Lock()
	defer m.watcherMutex.Unlock()

	if m.watcher != nil {
		m.logger.Fatal("Multiple calls to Watch() not supported")
	}
	m.watcher = fn
}

// Inform sends an update to the registered watcher function, if it is set.
func (m *MultiScaler) Inform(event types.NamespacedName) bool {
	m.watcherMutex.RLock()
	defer m.watcherMutex.RUnlock()

	if m.watcher != nil {
		m.watcher(event)
		return true
	}
	return false
}

func (m *MultiScaler) runScalerTicker(runner *scalerRunner) {
	metricKey := types.NamespacedName{Namespace: runner.decider.Namespace, Name: runner.decider.Name}
	ticker := m.tickProvider(tickInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-m.scalersStopCh:
				return
			case <-runner.stopCh:
				return
			case <-ticker.C:
				m.tickScaler(runner.scaler, runner, metricKey)
			case <-runner.pokeCh:
				m.tickScaler(runner.scaler, runner, metricKey)
			}
		}
	}()
}

func (m *MultiScaler) createScaler(ctx context.Context, decider *Decider) (*scalerRunner, error) {
	d := decider.DeepCopy()
	scaler, err := m.uniScalerFactory(d)
	if err != nil {
		return nil, err
	}

	runner := &scalerRunner{
		scaler:  scaler,
		stopCh:  make(chan struct{}),
		decider: d,
		pokeCh:  make(chan struct{}),
		logger:  logging.FromContext(ctx),
	}
	d.Status.DesiredScale = -1
	switch tbc := d.Spec.TargetBurstCapacity; tbc {
	case -1, 0:
		d.Status.ExcessBurstCapacity = int32(tbc)
	default:
		// If TBC > Target * InitialScale, then we know initial
		// scale won't be enough to cover TBC and we'll be behind activator.
		d.Status.ExcessBurstCapacity = int32(float64(d.Spec.InitialScale)*d.Spec.TotalValue - tbc)
	}

	m.runScalerTicker(runner)
	return runner, nil
}

func (m *MultiScaler) tickScaler(scaler UniScaler, runner *scalerRunner, metricKey types.NamespacedName) {
	sr := scaler.Scale(runner.logger, time.Now())

	if !sr.ScaleValid {
		return
	}

	if runner.updateLatestScale(sr) {
		m.Inform(metricKey)
	}
}

// Poke checks if the autoscaler needs to be run immediately.
func (m *MultiScaler) Poke(key types.NamespacedName, stat metrics.Stat) {
	m.scalersMutex.RLock()
	defer m.scalersMutex.RUnlock()

	scaler, exists := m.scalers[key]
	if !exists {
		return
	}

	if scaler.latestScale() == 0 && stat.AverageConcurrentRequests != 0 {
		scaler.pokeCh <- struct{}{}
	}
}
