/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gc

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	cm "knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
)

const (
	ConfigName = "config-gc"
)

type Config struct {
	// Delay duration after a revision create before considering it for GC
	StaleRevisionCreateDelay time.Duration
	// Timeout since a revision lastPinned before it should be GC'd
	// This must be longer than the controller resync period
	StaleRevisionTimeout time.Duration
	// Minimum number of generations of revisions to keep before considering for GC.
	StaleRevisionMinimumGenerations int64
	// Minimum staleness duration before updating lastPinned
	StaleRevisionLastpinnedDebounce time.Duration

	// Duration from creation when a Revision should be considered active
	// and exempt from GC. Note that GCMaxStaleRevision may override this if set.
	RetainSinceCreateTime time.Duration
	// Duration from last active when a Revision should be considered active
	// and exempt from GC.Note that GCMaxStaleRevision may override this if set.
	RetainSinceLastActiveTime time.Duration
	// Minimum number of generations of revisions to keep before considering for GC.
	// Set -1 to disable minimum and fill up to max.
	// Either min or max must be set.
	MinStaleRevisions int64
	// Maximum number of stale revisions to keep before considering for GC.
	// regardless of creation or staleness time-bounds
	// Set -1 to disable this setting.
	// Either min or max must be set.
	MaxStaleRevisions int64
}

func defaultConfig() *Config {
	return &Config{
		// V1 GC Settings
		StaleRevisionCreateDelay:        48 * time.Hour,
		StaleRevisionTimeout:            15 * time.Hour,
		StaleRevisionLastpinnedDebounce: 5 * time.Hour,
		StaleRevisionMinimumGenerations: 20,

		// V2 GC Settings
		RetainSinceCreateTime:     48 * time.Hour,
		RetainSinceLastActiveTime: 15 * time.Hour,
		MinStaleRevisions:         20,
		MaxStaleRevisions:         -1,
	}
}

func NewConfigFromConfigMapFunc(ctx context.Context) func(configMap *corev1.ConfigMap) (*Config, error) {
	logger := logging.FromContext(ctx)
	minRevisionTimeout := controller.GetResyncPeriod(ctx)
	return func(configMap *corev1.ConfigMap) (*Config, error) {
		c := defaultConfig()

		if err := cm.Parse(configMap.Data,
			cm.AsDuration("stale-revision-create-delay", &c.StaleRevisionCreateDelay),
			cm.AsDuration("stale-revision-timeout", &c.StaleRevisionTimeout),
			cm.AsDuration("stale-revision-lastpinned-debounce", &c.StaleRevisionLastpinnedDebounce),
			cm.AsInt64("stale-revision-minimum-generations", &c.StaleRevisionMinimumGenerations),

			cm.AsDuration("retain-since-create-time", &c.RetainSinceCreateTime),
			cm.AsDuration("retain-since-last-active-time", &c.RetainSinceLastActiveTime),
			cm.AsInt64("min-stale-revisions", &c.MinStaleRevisions),
			cm.AsInt64("max-stale-revisions", &c.MaxStaleRevisions),
		); err != nil {
			return nil, fmt.Errorf("failed to parse data: %w", err)
		}

		if c.MaxStaleRevisions >= 0 && c.MinStaleRevisions > c.MaxStaleRevisions {
			return nil, fmt.Errorf(
				"min-stale-revisions(%d) must be <= max-stale-revisions(%d)",
				c.MinStaleRevisions, c.MaxStaleRevisions)
		}
		if c.MinStaleRevisions < 0 {
			return nil, fmt.Errorf("min-stale-revisions must be non-negative, was: %d", c.MinStaleRevisions)
		}
		if c.MaxStaleRevisions < -1 {
			return nil, fmt.Errorf("max-stale-revisions must be >= -1, was: %d", c.MaxStaleRevisions)
		}

		if c.StaleRevisionMinimumGenerations < 0 {
			return nil, fmt.Errorf("stale-revision-minimum-generations must be non-negative, was: %d", c.StaleRevisionMinimumGenerations)
		}

		if c.StaleRevisionTimeout-c.StaleRevisionLastpinnedDebounce < minRevisionTimeout {
			logger.Warnf("Got revision timeout of %v, minimum supported value is %v",
				c.StaleRevisionTimeout, minRevisionTimeout+c.StaleRevisionLastpinnedDebounce)
			c.StaleRevisionTimeout = minRevisionTimeout + c.StaleRevisionLastpinnedDebounce
		}
		return c, nil
	}
}
