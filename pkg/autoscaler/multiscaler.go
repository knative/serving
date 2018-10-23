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
	"github.com/knative/pkg/logging/logkey"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/api/errors"

	kpa "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
)

const (
	// Enough buffer to store scale requests generated every 2
	// seconds while an http request is taking the full timeout of 5
	// second.
	scaleBufferSize = 10
)

type Metric struct {
	DesiredScale int32
}

// UniScaler records statistics for a particular KPA and proposes the scale for the KPA's target based on those statistics.
type UniScaler interface {
	// Record records the given statistics.
	Record(context.Context, Stat)

	// Scale either proposes a number of replicas or skips proposing. The proposal is requested at the given time.
	// The returned boolean is true if and only if a proposal was returned.
	Scale(context.Context, time.Time) (int32, bool)
}

// UniScalerFactory creates a UniScaler for a given KPA using the given dynamic configuration.
type UniScalerFactory func(*kpa.PodAutoscaler, *DynamicConfig) (UniScaler, error)

// scalerRunner wraps a UniScaler and a channel for implementing shutdown behavior.
type scalerRunner struct {
	scaler UniScaler
	stopCh chan struct{}

	// lsm guards access to latestScale
	lsm         sync.RWMutex
	latestScale int32
}

func (sr *scalerRunner) getLatestScale() int32 {
	sr.lsm.RLock()
	defer sr.lsm.RUnlock()
	return sr.latestScale
}

func (sr *scalerRunner) updateLatestScale(new int32) bool {
	sr.lsm.Lock()
	defer sr.lsm.Unlock()
	if sr.latestScale != new {
		sr.latestScale = new
		return true
	}
	return false
}

// NewKpaKey identifies a KPA in the multiscaler. Stats send in
// are identified and routed via this key.
func NewKpaKey(namespace string, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

// MultiScaler maintains a collection of UniScalers.
type MultiScaler struct {
	scalers       map[string]*scalerRunner
	scalersMutex  sync.RWMutex
	scalersStopCh <-chan struct{}

	dynConfig *DynamicConfig

	uniScalerFactory UniScalerFactory

	logger *zap.SugaredLogger

	watcher func(string)
}

// NewMultiScaler constructs a MultiScaler.
func NewMultiScaler(dynConfig *DynamicConfig, stopCh <-chan struct{}, uniScalerFactory UniScalerFactory, logger *zap.SugaredLogger) *MultiScaler {
	logger.Debugf("Creating MultiScaler with configuration %#v", dynConfig)
	return &MultiScaler{
		scalers:          make(map[string]*scalerRunner),
		scalersStopCh:    stopCh,
		dynConfig:        dynConfig,
		uniScalerFactory: uniScalerFactory,
		logger:           logger,
	}
}

func (m *MultiScaler) Get(ctx context.Context, key string) (*Metric, error) {
	m.scalersMutex.RLock()
	defer m.scalersMutex.RUnlock()
	scaler, exists := m.scalers[key]
	if !exists {
		// This GroupResource is a lie, but unfortunately this interface requires one.
		return nil, errors.NewNotFound(kpa.Resource("Metrics"), key)
	}
	return &Metric{
		DesiredScale: scaler.getLatestScale(),
	}, nil
}

func (m *MultiScaler) Create(ctx context.Context, kpa *kpa.PodAutoscaler) (*Metric, error) {
	m.scalersMutex.Lock()
	defer m.scalersMutex.Unlock()
	key := NewKpaKey(kpa.Namespace, kpa.Name)
	scaler, exists := m.scalers[key]
	if !exists {
		var err error
		scaler, err = m.createScaler(ctx, kpa)
		if err != nil {
			return nil, err
		}
		m.scalers[key] = scaler
	}
	return &Metric{
		DesiredScale: scaler.getLatestScale(),
	}, nil
}

func (m *MultiScaler) Delete(ctx context.Context, key string) error {
	m.scalersMutex.Lock()
	defer m.scalersMutex.Unlock()
	if scaler, exists := m.scalers[key]; exists {
		close(scaler.stopCh)
		delete(m.scalers, key)
	}
	return nil
}

func (m *MultiScaler) Watch(fn func(string)) {
	if m.watcher != nil {
		m.logger.Fatal("Multiple calls to Watch() not supported")
	}
	m.watcher = fn
}

func (m *MultiScaler) createScaler(ctx context.Context, kpa *kpa.PodAutoscaler) (*scalerRunner, error) {
	scaler, err := m.uniScalerFactory(kpa, m.dynConfig)
	if err != nil {
		return nil, err
	}

	stopCh := make(chan struct{})
	runner := &scalerRunner{scaler: scaler, latestScale: -1, stopCh: stopCh}

	ticker := time.NewTicker(m.dynConfig.Current().TickInterval)

	scaleChan := make(chan int32, scaleBufferSize)

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
				m.tickScaler(ctx, scaler, scaleChan)
			}
		}
	}()

	kpaKey := NewKpaKey(kpa.Namespace, kpa.Name)
	go func() {
		for {
			select {
			case <-m.scalersStopCh:
				return
			case <-stopCh:
				return
			case desiredScale := <-scaleChan:
				if runner.updateLatestScale(desiredScale) {
					m.watcher(kpaKey)
				}
			}
		}
	}()

	return runner, nil
}

func (m *MultiScaler) tickScaler(ctx context.Context, scaler UniScaler, scaleChan chan<- int32) {
	logger := logging.FromContext(ctx)
	desiredScale, scaled := scaler.Scale(ctx, time.Now())

	if scaled {
		// Cannot scale negative.
		if desiredScale < 0 {
			logger.Errorf("Cannot scale: desiredScale %d < 0.", desiredScale)
			return
		}

		// Don't scale to zero if scale to zero is disabled.
		if desiredScale == 0 && !m.dynConfig.Current().EnableScaleToZero {
			logger.Warn("Cannot scale: Desired scale == 0 && EnableScaleToZero == false.")
			return
		}

		scaleChan <- desiredScale
	}
}

// RecordStat records some statistics for the given KPA. kpaKey should have the
// form namespace/name.
func (m *MultiScaler) RecordStat(key string, stat Stat) {
	m.scalersMutex.RLock()
	defer m.scalersMutex.RUnlock()

	scaler, exists := m.scalers[key]
	if exists {
		logger := m.logger.With(zap.String(logkey.Key, key))
		ctx := logging.WithLogger(context.TODO(), logger)
		scaler.scaler.Record(ctx, stat)
	}
}
