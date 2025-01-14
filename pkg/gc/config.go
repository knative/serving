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

package gc

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	cm "knative.dev/pkg/configmap"
)

const (
	// ConfigName is the name of the config map for garbage collection.
	ConfigName = "config-gc"

	// Disabled is the value (-1) used by various config map values to indicate
	// the setting is disabled.
	Disabled = -1

	disabled = "disabled"
)

// Config defines the tunable parameters for Garbage Collection.
type Config struct {
	// Duration from creation when a Revision should be considered active
	// and exempt from GC. Note that GCMaxStaleRevision may override this if set.
	// Set Disabled (-1) to disable/ignore duration and always consider active.
	RetainSinceCreateTime time.Duration
	// Duration from last active when a Revision should be considered active
	// and exempt from GC.Note that GCMaxStaleRevision may override this if set.
	// Set Disabled (-1) to disable/ignore duration and always consider active.
	RetainSinceLastActiveTime time.Duration
	// Minimum number of non-active revisions to keep before considering for GC.
	MinNonActiveRevisions int64
	// Maximum number of non-active revisions to keep before considering for GC.
	// regardless of creation or staleness time-bounds.
	// Set Disabled (-1) to disable/ignore max.
	MaxNonActiveRevisions int64
}

func defaultConfig() *Config {
	return &Config{
		RetainSinceCreateTime:     48 * time.Hour,
		RetainSinceLastActiveTime: 15 * time.Hour,
		MinNonActiveRevisions:     20,
		MaxNonActiveRevisions:     1000,
	}
}

// NewConfigFromConfigMapFunc creates a Config from the supplied ConfigMap func.
func NewConfigFromConfigMapFunc(ctx context.Context) func(configMap *corev1.ConfigMap) (*Config, error) {
	return func(configMap *corev1.ConfigMap) (*Config, error) {
		c := defaultConfig()

		var retainCreate, retainActive, max string
		if err := cm.Parse(configMap.Data,
			cm.AsString("retain-since-create-time", &retainCreate),
			cm.AsString("retain-since-last-active-time", &retainActive),
			cm.AsInt64("min-non-active-revisions", &c.MinNonActiveRevisions),
			cm.AsString("max-non-active-revisions", &max),
		); err != nil {
			return nil, fmt.Errorf("failed to parse data: %w", err)
		}

		// validate V2 settings
		if err := parseDisabledOrDuration(retainCreate, &c.RetainSinceCreateTime); err != nil {
			return nil, fmt.Errorf("failed to parse retain-since-create-time: %w", err)
		}
		if err := parseDisabledOrDuration(retainActive, &c.RetainSinceLastActiveTime); err != nil {
			return nil, fmt.Errorf("failed to parse retain-since-last-active-time: %w", err)
		}
		if err := parseDisabledOrInt64(max, &c.MaxNonActiveRevisions); err != nil {
			return nil, fmt.Errorf("failed to parse max-non-active-revisions: %w", err)
		}
		if c.MinNonActiveRevisions < 0 {
			return nil, fmt.Errorf("min-non-active-revisions must be non-negative, was: %d", c.MinNonActiveRevisions)
		}
		if c.MaxNonActiveRevisions >= 0 && c.MinNonActiveRevisions > c.MaxNonActiveRevisions {
			return nil, fmt.Errorf("min-non-active-revisions(%d) must be <= max-non-active-revisions(%d)", c.MinNonActiveRevisions, c.MaxNonActiveRevisions)
		}
		return c, nil
	}
}

func parseDisabledOrInt64(val string, toSet *int64) error {
	switch {
	case val == "":
		// keep default value
	case strings.EqualFold(val, disabled):
		*toSet = Disabled
	default:
		parsed, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			return err
		}
		if parsed > math.MaxInt64 {
			return fmt.Errorf("value should be lower than %v", math.MaxInt64)
		}
		*toSet = int64(parsed)
	}
	return nil
}

func parseDisabledOrDuration(val string, toSet *time.Duration) error {
	switch {
	case val == "":
		// keep default value
	case strings.EqualFold(val, disabled):
		*toSet = time.Duration(Disabled)
	default:
		parsed, err := time.ParseDuration(val)
		if err != nil {
			return err
		}
		if parsed < 0 {
			return errors.New("must be non-negative")
		}
		*toSet = parsed
	}
	return nil
}
