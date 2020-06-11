/*
Copyright 2020 The Knative Authors

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

package config

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	cm "knative.dev/pkg/configmap"
)

const (
	// DefaultsConfigName is the name of config map for the defaults.
	DefaultsConfigName = "config-defaults"

	// DefaultRevisionTimeoutSeconds will be set if timeoutSeconds not specified.
	DefaultRevisionTimeoutSeconds = 5 * 60

	// DefaultMaxRevisionTimeoutSeconds will be set if MaxRevisionTimeoutSeconds is not specified.
	DefaultMaxRevisionTimeoutSeconds = 10 * 60
)

func defaultConfig() *Defaults {
	return &Defaults{
		RevisionTimeoutSeconds:    DefaultRevisionTimeoutSeconds,
		MaxRevisionTimeoutSeconds: DefaultMaxRevisionTimeoutSeconds,
	}
}

// NewDefaultsConfigFromMap creates a Defaults from the supplied Map.
func NewDefaultsConfigFromMap(data map[string]string) (*Defaults, error) {
	nc := defaultConfig()

	if err := cm.Parse(data,
		cm.AsInt64("revision-timeout-seconds", &nc.RevisionTimeoutSeconds),
		cm.AsInt64("max-revision-timeout-seconds", &nc.MaxRevisionTimeoutSeconds),
	); err != nil {
		return nil, err
	}

	if nc.RevisionTimeoutSeconds > nc.MaxRevisionTimeoutSeconds {
		return nil, fmt.Errorf("revision-timeout-seconds (%d) cannot be greater than max-revision-timeout-seconds (%d)", nc.RevisionTimeoutSeconds, nc.MaxRevisionTimeoutSeconds)
	}
	return nc, nil
}

// NewDefaultsConfigFromConfigMap creates a Defaults from the supplied configMap.
func NewDefaultsConfigFromConfigMap(config *corev1.ConfigMap) (*Defaults, error) {
	return NewDefaultsConfigFromMap(config.Data)
}

// Defaults includes the default values to be populated by the webhook.
type Defaults struct {
	RevisionTimeoutSeconds int64
	// This is the timeout set for ingress.
	// RevisionTimeoutSeconds must be less than this value.
	MaxRevisionTimeoutSeconds int64
}

// asQuantity parses the value at key as a *resource.Quantity into the target, if it exists.
func asQuantity(key string, target **resource.Quantity) cm.ParseFunc {
	return func(data map[string]string) error {
		if raw, ok := data[key]; !ok {
			*target = nil
		} else if val, err := resource.ParseQuantity(raw); err != nil {
			return err
		} else {
			*target = &val
		}
		return nil
	}
}
