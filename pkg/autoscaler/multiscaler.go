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
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
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

// UniScaler records statistics for a particular revision and proposes the scale for the revision based on those statistics.
type UniScaler interface {
	// Record records the given statistics.
	Record(context.Context, Stat)

	// Scale either proposes a number of replicas or skips proposing. The proposal is requested at the given time.
	// The returned boolean is true if and only if a proposal was returned.
	Scale(context.Context, time.Time) (int32, bool)
}

// UniScalerFactory creates a UniScaler for a given revision using the given configuration.
type UniScalerFactory func(*v1alpha1.Revision, *Config) (UniScaler, error)

// RevisionScaler knows how to scale revisions.
type RevisionScaler interface {
	// Scale attempts to scale the given revision to the desired scale.
	Scale(rev *v1alpha1.Revision, desiredScale int32)
}

// scalerRunner wraps a UniScaler and a channel for implementing shutdown behavior.
type scalerRunner struct {
	scaler UniScaler
	stopCh chan struct{}
}

type revisionKey string

func newRevisionKey(namespace string, name string) revisionKey {
	return revisionKey(fmt.Sprintf("%s/%s", namespace, name))
}

// MultiScaler maintains a collection of UniScalers indexed by revisionKey.
type MultiScaler struct {
	scalers       map[revisionKey]*scalerRunner
	scalersMutex  sync.RWMutex
	scalersStopCh <-chan struct{}

	config *Config

	revisionScaler RevisionScaler

	uniScalerFactory UniScalerFactory

	logger *zap.SugaredLogger
}

// NewMultiScaler constructs a MultiScaler.
func NewMultiScaler(config *Config, revisionScaler RevisionScaler, stopCh <-chan struct{}, uniScalerFactory UniScalerFactory, logger *zap.SugaredLogger) *MultiScaler {
	logger.Debugf("Creating MultiScalar with configuration %#v", config)
	return &MultiScaler{
		scalers:          make(map[revisionKey]*scalerRunner),
		scalersStopCh:    stopCh,
		config:           config,
		revisionScaler:   revisionScaler,
		uniScalerFactory: uniScalerFactory,
		logger:           logger,
	}
}

// OnPresent adds, if necessary, a scaler for the given revision.
func (m *MultiScaler) OnPresent(rev *v1alpha1.Revision, logger *zap.SugaredLogger) {
	m.scalersMutex.Lock()
	defer m.scalersMutex.Unlock()
	key := newRevisionKey(rev.Namespace, rev.Name)
	if _, exists := m.scalers[key]; !exists {
		ctx := logging.WithLogger(context.TODO(), logger)
		logger.Debug("Creating scaler for revision.")
		scaler, err := m.createScaler(ctx, rev)
		if err != nil {
			logger.Errorf("Failed to create scaler for revision %#v: %v", rev, err)
			return
		}
		logger.Info("Created scaler for revision.")
		m.scalers[key] = scaler
	}
}

// OnAbsent removes, if necessary, a scaler for the revision in the given namespace and with the given name.
func (m *MultiScaler) OnAbsent(namespace string, name string, logger *zap.SugaredLogger) {
	m.scalersMutex.Lock()
	defer m.scalersMutex.Unlock()
	key := newRevisionKey(namespace, name)
	if scaler, exists := m.scalers[key]; exists {
		close(scaler.stopCh)
		delete(m.scalers, key)
		logger.Info("Deleted scaler for revision.")
	}
}

// loggerWithRevisionInfo enriches the logs with revision name and namespace.
func loggerWithRevisionInfo(logger *zap.SugaredLogger, ns string, name string) *zap.SugaredLogger {
	return logger.With(zap.String(commonlogkey.Namespace, ns), zap.String(logkey.Revision, name))
}

func (m *MultiScaler) createScaler(ctx context.Context, rev *v1alpha1.Revision) (*scalerRunner, error) {
	scaler, err := m.uniScalerFactory(rev, m.config)
	if err != nil {
		return nil, err
	}

	stopCh := make(chan struct{})
	runner := &scalerRunner{scaler: scaler, stopCh: stopCh}

	ticker := time.NewTicker(m.config.TickInterval)

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
				m.revisionScaler.Scale(rev, mostRecentDesiredScale(desiredScale, scaleChan, logger))
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
		if desiredScale == 0 && !m.config.EnableScaleToZero {
			logger.Warn("Cannot scale: Desired scale == 0 && EnableScaleToZero == false.")
			return
		}

		scaleChan <- desiredScale
	}
}

// RecordStat records some statistics for the given revision. revKey should have the
// form namespace/name.
func (m *MultiScaler) RecordStat(revKey string, stat Stat) {
	m.scalersMutex.RLock()
	defer m.scalersMutex.RUnlock()

	scaler, exists := m.scalers[revisionKey(revKey)]
	if exists {
		namespace, name, err := cache.SplitMetaNamespaceKey(revKey)
		if err != nil {
			m.logger.Errorf("Invalid revision key %s", revKey)
			return
		}
		logger := loggerWithRevisionInfo(m.logger, namespace, name)
		ctx := logging.WithLogger(context.TODO(), logger)
		scaler.scaler.Record(ctx, stat)
	}
}
