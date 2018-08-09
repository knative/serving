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

	commonlogkey "github.com/knative/pkg/logging/logkey"
	kpa "github.com/knative/serving/pkg/apis/autoscaling/v1alpha1"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/logging/logkey"
	"go.uber.org/zap"
	"k8s.io/client-go/tools/cache"
)

const (
	// Enough buffer to store scale requests generated every 2
	// seconds while an http request is taking the full timeout of 5
	// second.
	scaleBufferSize = 10
)

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

// KPAScaler knows how to scale the targets of KPAs
type KPAScaler interface {
	// Scale attempts to scale the given KPA's target to the desired scale.
	Scale(kpa *kpa.PodAutoscaler, desiredScale int32)
}

// scalerRunner wraps a UniScaler and a channel for implementing shutdown behavior.
type scalerRunner struct {
	scaler UniScaler
	stopCh chan struct{}
}

type kpaKey string

func newKPAKey(namespace string, name string) kpaKey {
	return kpaKey(fmt.Sprintf("%s/%s", namespace, name))
}

// MultiScaler maintains a collection of UniScalers indexed by kpaKey.
type MultiScaler struct {
	scalers       map[kpaKey]*scalerRunner
	scalersMutex  sync.RWMutex
	scalersStopCh <-chan struct{}

	dynConfig *DynamicConfig

	kpaScaler KPAScaler

	uniScalerFactory UniScalerFactory

	logger *zap.SugaredLogger
}

// NewMultiScaler constructs a MultiScaler.
func NewMultiScaler(dynConfig *DynamicConfig, kpaScaler KPAScaler, stopCh <-chan struct{}, uniScalerFactory UniScalerFactory, logger *zap.SugaredLogger) *MultiScaler {
	logger.Debugf("Creating MultiScalar with configuration %#v", dynConfig)
	return &MultiScaler{
		scalers:          make(map[kpaKey]*scalerRunner),
		scalersStopCh:    stopCh,
		dynConfig:        dynConfig,
		kpaScaler:        kpaScaler,
		uniScalerFactory: uniScalerFactory,
		logger:           logger,
	}
}

// OnPresent adds, if necessary, a scaler for the given kpa.
func (m *MultiScaler) OnPresent(kpa *kpa.PodAutoscaler, logger *zap.SugaredLogger) {
	m.scalersMutex.Lock()
	defer m.scalersMutex.Unlock()
	key := newKPAKey(kpa.Namespace, kpa.Name)
	if _, exists := m.scalers[key]; !exists {
		ctx := logging.WithLogger(context.TODO(), logger)
		logger.Debug("Creating scaler for KPA.")
		scaler, err := m.createScaler(ctx, kpa)
		if err != nil {
			logger.Errorf("Failed to create scaler for KPA %#v: %v", kpa, err)
			return
		}
		logger.Info("Created scaler for KPA.")
		m.scalers[key] = scaler
	}
}

// OnAbsent removes, if necessary, a scaler for the KPA in the given namespace and with the given name.
func (m *MultiScaler) OnAbsent(namespace string, name string, logger *zap.SugaredLogger) {
	m.scalersMutex.Lock()
	defer m.scalersMutex.Unlock()
	key := newKPAKey(namespace, name)
	if scaler, exists := m.scalers[key]; exists {
		close(scaler.stopCh)
		delete(m.scalers, key)
		logger.Info("Deleted scaler for KPA.")
	}
}

// loggerWithKPAInfo enriches the logs with KPA name and namespace.
func loggerWithKPAInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(commonlogkey.Namespace, ns), zap.String(logkey.KPA, name))
}

func (m *MultiScaler) createScaler(ctx context.Context, kpa *kpa.PodAutoscaler) (*scalerRunner, error) {
	scaler, err := m.uniScalerFactory(kpa, m.dynConfig)
	if err != nil {
		return nil, err
	}

	stopCh := make(chan struct{})
	runner := &scalerRunner{scaler: scaler, stopCh: stopCh}

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

	logger := logging.FromContext(ctx)

	go func() {
		for {
			select {
			case <-m.scalersStopCh:
				return
			case <-stopCh:
				return
			case desiredScale := <-scaleChan:
				m.kpaScaler.Scale(kpa, mostRecentDesiredScale(desiredScale, scaleChan, logger))
			}
		}
	}()

	return runner, nil
}

func mostRecentDesiredScale(desiredScale int32, scaleChan chan int32, logger *zap.SugaredLogger) int32 {
	for {
		select {
		case desiredScale = <-scaleChan:
			logger.Info("Scaling is not keeping up with autoscaling requests")
		default:
			// scaleChan is empty
			return desiredScale
		}
	}
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

	scaler, exists := m.scalers[kpaKey(key)]
	if exists {
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			m.logger.Errorf("Invalid KPA key %s", key)
			return
		}
		logger := loggerWithKPAInfo(m.logger, namespace, name)
		ctx := logging.WithLogger(context.TODO(), logger)
		scaler.scaler.Record(ctx, stat)
	}
}
