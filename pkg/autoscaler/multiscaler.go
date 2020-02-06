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
	"math"
	"sync"
	"time"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/logging/logkey"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
)

// Decider is a resource which observes the request load of a Revision and
// recommends a number of replicas to run.
// +k8s:deepcopy-gen=true
type Decider struct {
	metav1.ObjectMeta
	Spec   DeciderSpec
	Status DeciderStatus
}

// DeciderSpec is the parameters in which the Revision should scaled.
type DeciderSpec struct {
	TickInterval     time.Duration
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
	PanicThreshold      float64
	// StableWindow is needed to determine when to exit panicmode.
	StableWindow time.Duration
	// The name of the k8s service for pod information.
	ServiceName string
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
}

// UniScaler records statistics for a particular Decider and proposes the scale for the Decider's target based on those statistics.
type UniScaler interface {
	// Scale either proposes a number of replicas and available excess burst capacity,
	// or skips proposing. The proposal is requested at the given time.
	// The returned boolean is true if and only if a proposal was returned.
	Scale(context.Context, time.Time) (int32, int32, bool)

	// Update reconfigures the UniScaler according to the DeciderSpec.
	Update(*DeciderSpec) error
}

// UniScalerFactory creates a UniScaler for a given PA using the given dynamic configuration.
type UniScalerFactory func(*Decider) (UniScaler, error)

// scalerRunner wraps a UniScaler and a channel for implementing shutdown behavior.
type scalerRunner struct {
	scaler UniScaler
	stopCh chan struct{}
	pokeCh chan struct{}

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

func (sr *scalerRunner) updateLatestScale(proposed, ebc int32) bool {
	ret := false
	sr.mux.Lock()
	defer sr.mux.Unlock()
	if sr.decider.Status.DesiredScale != proposed {
		sr.decider.Status.DesiredScale = proposed
		ret = true
	}

	// If sign has changed -- then we have to update KPA
	ret = ret || !sameSign(sr.decider.Status.ExcessBurstCapacity, ebc)

	// Update with the latest calculation anyway.
	sr.decider.Status.ExcessBurstCapacity = ebc
	return ret
}

// MultiScaler maintains a collection of Uniscalers.
type MultiScaler struct {
	scalers       map[types.NamespacedName]*scalerRunner
	scalersMutex  sync.RWMutex
	scalersStopCh <-chan struct{}

	uniScalerFactory UniScalerFactory

	logger *zap.SugaredLogger

	watcher      func(types.NamespacedName)
	watcherMutex sync.RWMutex

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
func (m *MultiScaler) Get(ctx context.Context, namespace, name string) (*Decider, error) {
	key := types.NamespacedName{Namespace: namespace, Name: name}
	m.scalersMutex.RLock()
	defer m.scalersMutex.RUnlock()
	scaler, exists := m.scalers[key]
	if !exists {
		// This GroupResource is a lie, but unfortunately this interface requires one.
		return nil, errors.NewNotFound(av1alpha1.Resource("Deciders"), key.String())
	}
	scaler.mux.RLock()
	defer scaler.mux.RUnlock()
	return scaler.decider.DeepCopy(), nil
}

// Create instantiates the desired Decider.
func (m *MultiScaler) Create(ctx context.Context, decider *Decider) (*Decider, error) {
	key := types.NamespacedName{Namespace: decider.Namespace, Name: decider.Name}
	logger := m.logger.With(zap.String(logkey.Key, key.String()))
	ctx = logging.WithLogger(ctx, logger)
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
	scaler.mux.RLock()
	defer scaler.mux.RUnlock()
	// scaler.decider is already a copy of the original, so just return it.
	return scaler.decider, nil
}

// Update applied the desired DeciderSpec to a currently running Decider.
func (m *MultiScaler) Update(ctx context.Context, decider *Decider) (*Decider, error) {
	key := types.NamespacedName{Namespace: decider.Namespace, Name: decider.Name}
	logger := m.logger.With(zap.String(logkey.Key, key.String()))
	ctx = logging.WithLogger(ctx, logger)
	m.scalersMutex.Lock()
	defer m.scalersMutex.Unlock()
	if scaler, exists := m.scalers[key]; exists {
		scaler.mux.Lock()
		defer scaler.mux.Unlock()
		oldDeciderSpec := scaler.decider.Spec
		// Make sure we store the copy.
		scaler.decider = decider.DeepCopy()
		scaler.scaler.Update(&decider.Spec)
		if oldDeciderSpec.TickInterval != decider.Spec.TickInterval {
			m.updateRunner(ctx, scaler)
		}
		return decider, nil
	}
	// This GroupResource is a lie, but unfortunately this interface requires one.
	return nil, errors.NewNotFound(av1alpha1.Resource("Deciders"), key.String())
}

// Delete stops and removes a Decider.
func (m *MultiScaler) Delete(ctx context.Context, namespace, name string) error {
	key := types.NamespacedName{Namespace: namespace, Name: name}
	m.scalersMutex.Lock()
	defer m.scalersMutex.Unlock()
	if scaler, exists := m.scalers[key]; exists {
		close(scaler.stopCh)
		delete(m.scalers, key)
	}
	return nil
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

func (m *MultiScaler) updateRunner(ctx context.Context, runner *scalerRunner) {
	runner.stopCh <- struct{}{}
	m.runScalerTicker(ctx, runner)
}

func (m *MultiScaler) runScalerTicker(ctx context.Context, runner *scalerRunner) {
	metricKey := types.NamespacedName{Namespace: runner.decider.Namespace, Name: runner.decider.Name}
	ticker := m.tickProvider(runner.decider.Spec.TickInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-m.scalersStopCh:
				return
			case <-runner.stopCh:
				return
			case <-ticker.C:
				m.tickScaler(ctx, runner.scaler, runner, metricKey)
			case <-runner.pokeCh:
				m.tickScaler(ctx, runner.scaler, runner, metricKey)
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
	}
	d.Status.DesiredScale = -1
	switch tbc := d.Spec.TargetBurstCapacity; tbc {
	case -1, 0:
		d.Status.ExcessBurstCapacity = int32(tbc)
	default:
		// If TBC > Target * InitialScale (currently 1), then we know initial
		// scale won't be enough to cover TBC and we'll be behind activator.
		// TODO(autoscale-wg): fix this when we switch to non "1" initial scale.
		d.Status.ExcessBurstCapacity = int32(1*d.Spec.TotalValue - tbc)
	}

	m.runScalerTicker(ctx, runner)
	return runner, nil
}

func (m *MultiScaler) tickScaler(ctx context.Context, scaler UniScaler, runner *scalerRunner, metricKey types.NamespacedName) {
	logger := logging.FromContext(ctx)
	desiredScale, excessBC, scaled := scaler.Scale(ctx, time.Now())

	if !scaled {
		return
	}

	// Cannot scale negative (nor we can compute burst capacity).
	if desiredScale < 0 {
		logger.Errorf("Cannot scale: desiredScale %d < 0.", desiredScale)
		return
	}

	if runner.updateLatestScale(desiredScale, excessBC) {
		m.Inform(metricKey)
	}
}

// Poke checks if the autoscaler needs to be run immediately.
func (m *MultiScaler) Poke(key types.NamespacedName, stat Stat) {
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
