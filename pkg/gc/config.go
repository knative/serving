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
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
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
	// Minimum number of generations of revisions to keep before considering for GC
	StaleRevisionMinimumGenerations int64
	// Minimum staleness duration before updating lastPinned
	StaleRevisionLastpinnedDebounce time.Duration
}

func defaultConfig() *Config {
	return &Config{
		StaleRevisionCreateDelay:        48 * time.Hour,
		StaleRevisionTimeout:            15 * time.Hour,
		StaleRevisionLastpinnedDebounce: 5 * time.Hour,
		StaleRevisionMinimumGenerations: 20,
	}
}

func NewConfigFromConfigMapFunc(ctx context.Context) func(configMap *corev1.ConfigMap) (*Config, error) {
	logger := logging.FromContext(ctx)
	minRevisionTimeout := controller.GetResyncPeriod(ctx)
	return func(configMap *corev1.ConfigMap) (*Config, error) {
		c := defaultConfig()

		for _, dur := range []struct {
			key   string
			field *time.Duration
		}{{
			key:   "stale-revision-create-delay",
			field: &c.StaleRevisionCreateDelay,
		}, {
			key:   "stale-revision-timeout",
			field: &c.StaleRevisionTimeout,
		}, {
			key:   "stale-revision-lastpinned-debounce",
			field: &c.StaleRevisionLastpinnedDebounce,
		}} {
			if raw, ok := configMap.Data[dur.key]; ok {
				val, err := time.ParseDuration(raw)
				if err != nil {
					return nil, err
				}
				*dur.field = val
			}
		}

		if raw, ok := configMap.Data["stale-revision-minimum-generations"]; ok {
			val, err := strconv.ParseInt(raw, 10 /*base*/, 64 /*bit count*/)
			if err != nil {
				return nil, err
			} else if val < 0 {
				return nil, fmt.Errorf("stale-revision-minimum-generations must be non-negative, was: %d", val)
			}
			c.StaleRevisionMinimumGenerations = val
		}

		if c.StaleRevisionTimeout-c.StaleRevisionLastpinnedDebounce < minRevisionTimeout {
			logger.Warnf("Got revision timeout of %v, minimum supported value is %v", c.StaleRevisionTimeout, minRevisionTimeout+c.StaleRevisionLastpinnedDebounce)
			c.StaleRevisionTimeout = minRevisionTimeout + c.StaleRevisionLastpinnedDebounce
		}
		return c, nil
	}
}
