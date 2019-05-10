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
	"fmt"
	"sync"
	"time"

	"github.com/knative/pkg/logging"
	kpa "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	TickInterval      time.Duration
	MaxScaleUpRate    float64
	TargetConcurrency float64
	PanicThreshold    float64
	// TODO: remove MetricSpec when the custom metrics adapter implements Metric.
	MetricSpec MetricSpec

	// The name of the k8s service for pod information.
	ServiceName string
}

// DeciderStatus is the current scale recommendation.
type DeciderStatus struct {
	DesiredScale int32
}

// UniScaler records statistics for a particular Decider and proposes the scale for the Decider's target based on those statistics.
type UniScaler interface {
	// Scale either proposes a number of replicas or skips proposing. The proposal is requested at the given time.
	// The returned boolean is true if and only if a proposal was returned.
	Scale(context.Context, time.Time) (int32, bool)

	// Update reconfigures the UniScaler according to the DeciderSpec.
	Update(DeciderSpec) error
}

// UniScalerFactory creates a UniScaler for a given PA using the given dynamic configuration.
type UniScalerFactory func(*Decider) (UniScaler, error)

// scalerRunner wraps a UniScaler and a channel for implementing shutdown behavior.
type scalerRunner struct {
	scaler UniScaler
	stopCh chan struct{}
	pokeCh chan struct{}

	// mux guards access to metric
	mux     sync.RWMutex
	decider Decider
}

func (sr *scalerRunner) getLatestScale() int32 {
	sr.mux.RLock()
	defer sr.mux.RUnlock()
	return sr.decider.Status.DesiredScale
}

func (sr *scalerRunner) updateLatestScale(new int32) bool {
	sr.mux.Lock()
	defer sr.mux.Unlock()
	if sr.decider.Status.DesiredScale != new {
		sr.decider.Status.DesiredScale = new
		return true
	}
	return false
}

// NewMetricKey identifies a UniScaler in the multiscaler. Stats send in
// are identified and routed via this key.
func NewMetricKey(namespace string, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

// MultiScaler maintains a collection of Uniscalers.
type MultiScaler struct {
	scalers       map[string]*scalerRunner
	scalersMutex  sync.RWMutex
	scalersStopCh <-chan struct{}

	uniScalerFactory UniScalerFactory

	logger *zap.SugaredLogger

	watcher      func(string)
	watcherMutex sync.RWMutex
}

// NewMultiScaler constructs a MultiScaler.
func NewMultiScaler(
	stopCh <-chan struct{},
	uniScalerFactory UniScalerFactory,
	logger *zap.SugaredLogger) *MultiScaler {
	return &MultiScaler{
		scalers:          make(map[string]*scalerRunner),
		scalersStopCh:    stopCh,
		uniScalerFactory: uniScalerFactory,
		logger:           logger,
	}
}

// Get return the current Decider.
func (m *MultiScaler) Get(ctx context.Context, namespace, name string) (*Decider, error) {
	key := NewMetricKey(namespace, name)
	m.scalersMutex.RLock()
	defer m.scalersMutex.RUnlock()
	scaler, exists := m.scalers[key]
	if !exists {
		// This GroupResource is a lie, but unfortunately this interface requires one.
		return nil, errors.NewNotFound(kpa.Resource("Deciders"), key)
	}
	scaler.mux.RLock()
	defer scaler.mux.RUnlock()
	return (&scaler.decider).DeepCopy(), nil
}

// Create instantiates the desired Decider.
func (m *MultiScaler) Create(ctx context.Context, decider *Decider) (*Decider, error) {
	m.scalersMutex.Lock()
	defer m.scalersMutex.Unlock()
	key := NewMetricKey(decider.Namespace, decider.Name)
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
	return (&scaler.decider).DeepCopy(), nil
}

// Update applied the desired DeciderSpec to a currently running Decider.
func (m *MultiScaler) Update(ctx context.Context, decider *Decider) (*Decider, error) {
	key := NewMetricKey(decider.Namespace, decider.Name)
	m.scalersMutex.Lock()
	defer m.scalersMutex.Unlock()
	if scaler, exists := m.scalers[key]; exists {
		scaler.mux.Lock()
		defer scaler.mux.Unlock()
		scaler.decider = *decider
		scaler.scaler.Update(decider.Spec)
		return decider, nil
	}
	// This GroupResource is a lie, but unfortunately this interface requires one.
	return nil, errors.NewNotFound(kpa.Resource("Deciders"), key)
}

// Delete stops and removes a Decider.
func (m *MultiScaler) Delete(ctx context.Context, namespace, name string) error {
	key := NewMetricKey(namespace, name)
	m.scalersMutex.Lock()
	defer m.scalersMutex.Unlock()
	if scaler, exists := m.scalers[key]; exists {
		close(scaler.stopCh)
		delete(m.scalers, key)
	}
	return nil
}

// Watch registers a singleton function to call when DeciderStatus is updated.
func (m *MultiScaler) Watch(fn func(string)) {
	m.watcherMutex.Lock()
	defer m.watcherMutex.Unlock()

	if m.watcher != nil {
		m.logger.Fatal("Multiple calls to Watch() not supported")
	}
	m.watcher = fn
}

// Inform sends an update to the registered watcher function, if it is set.
func (m *MultiScaler) Inform(event string) bool {
	m.watcherMutex.RLock()
	defer m.watcherMutex.RUnlock()

	if m.watcher != nil {
		m.watcher(event)
		return true
	}
	return false
}

func (m *MultiScaler) createScaler(ctx context.Context, decider *Decider) (*scalerRunner, error) {
	scaler, err := m.uniScalerFactory(decider)
	if err != nil {
		return nil, err
	}

	stopCh := make(chan struct{})
	runner := &scalerRunner{
		scaler:  scaler,
		stopCh:  stopCh,
		decider: *decider,
		pokeCh:  make(chan struct{}),
	}
	runner.decider.Status.DesiredScale = -1
	metricKey := NewMetricKey(decider.Namespace, decider.Name)

	// TODO(#3977): Make sure this is reconciled if the tick interval changes.
	ticker := time.NewTicker(decider.Spec.TickInterval)
	go func() {
		for {
			select {
			case <-m.scalersStopCh:
				ticker.Stop()
				return
			case <-stopCh:
				ticker.Stop()
				return
			case <-ticker.C:
				m.tickScaler(ctx, scaler, runner, metricKey)
			case <-runner.pokeCh:
				m.tickScaler(ctx, scaler, runner, metricKey)
			}
		}
	}()

	return runner, nil
}

func (m *MultiScaler) tickScaler(ctx context.Context, scaler UniScaler, runner *scalerRunner, metricKey string) {
	logger := logging.FromContext(ctx)
	desiredScale, scaled := scaler.Scale(ctx, time.Now())

	if !scaled {
		return
	}

	// Cannot scale negative.
	if desiredScale < 0 {
		logger.Errorf("Cannot scale: desiredScale %d < 0.", desiredScale)
		return
	}

	if runner.updateLatestScale(desiredScale) {
		m.Inform(metricKey)
	}
}

// Poke checks if the autoscaler needs to be run immediately.
func (m *MultiScaler) Poke(key string, stat Stat) {
	m.scalersMutex.RLock()
	defer m.scalersMutex.RUnlock()

	scaler, exists := m.scalers[key]
	if !exists {
		return
	}

	if scaler.getLatestScale() == 0 && stat.AverageConcurrentRequests != 0 {
		scaler.pokeCh <- struct{}{}
	}
}
