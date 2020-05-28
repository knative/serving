/*
Copyright 2019 The Knative Authors

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
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/apis"
	cm "knative.dev/pkg/configmap"
)

const (
	// DefaultsConfigName is the name of config map for the defaults.
	DefaultsConfigName = "config-defaults"

	// DefaultRevisionTimeoutSeconds will be set if timeoutSeconds not specified.
	DefaultRevisionTimeoutSeconds = 5 * 60

	// DefaultMaxRevisionTimeoutSeconds will be set if MaxRevisionTimeoutSeconds is not specified.
	DefaultMaxRevisionTimeoutSeconds = 10 * 60

	// DefaultUserContainerName is the default name we give to the container
	// specified by the user, if `name:` is omitted.
	DefaultUserContainerName = "user-container"

	// DefaultContainerConcurrency is the default container concurrency. It will be set if ContainerConcurrency is not specified.
	DefaultContainerConcurrency int64 = 0

	// DefaultMaxRevisionContainerConcurrency is the maximum configurable
	// container concurrency.
	DefaultMaxRevisionContainerConcurrency int64 = 1000

	// DefaultAllowContainerConcurrencyZero is whether, by default,
	// containerConcurrency can be set to zero (i.e. unbounded) by users.
	DefaultAllowContainerConcurrencyZero bool = true
)

func defaultDefaultsConfig() *Defaults {
	return &Defaults{
		RevisionTimeoutSeconds:        DefaultRevisionTimeoutSeconds,
		MaxRevisionTimeoutSeconds:     DefaultMaxRevisionTimeoutSeconds,
		UserContainerNameTemplate:     DefaultUserContainerName,
		ContainerConcurrency:          DefaultContainerConcurrency,
		ContainerConcurrencyMaxLimit:  DefaultMaxRevisionContainerConcurrency,
		AllowContainerConcurrencyZero: DefaultAllowContainerConcurrencyZero,
	}
}

// NewDefaultsConfigFromMap creates a Defaults from the supplied Map.
func NewDefaultsConfigFromMap(data map[string]string) (*Defaults, error) {
	nc := defaultDefaultsConfig()

	if err := cm.Parse(data,
		cm.AsString("container-name-template", &nc.UserContainerNameTemplate),

		cm.AsBool("allow-container-concurrency-zero", &nc.AllowContainerConcurrencyZero),

		cm.AsInt64("revision-timeout-seconds", &nc.RevisionTimeoutSeconds),
		cm.AsInt64("max-revision-timeout-seconds", &nc.MaxRevisionTimeoutSeconds),
		cm.AsInt64("container-concurrency", &nc.ContainerConcurrency),
		cm.AsInt64("container-concurrency-max-limit", &nc.ContainerConcurrencyMaxLimit),

		asQuantity("revision-cpu-request", &nc.RevisionCPURequest),
		asQuantity("revision-memory-request", &nc.RevisionMemoryRequest),
		asQuantity("revision-cpu-limit", &nc.RevisionCPULimit),
		asQuantity("revision-memory-limit", &nc.RevisionMemoryLimit),
	); err != nil {
		return nil, err
	}

	if nc.RevisionTimeoutSeconds > nc.MaxRevisionTimeoutSeconds {
		return nil, fmt.Errorf("revision-timeout-seconds (%d) cannot be greater than max-revision-timeout-seconds (%d)", nc.RevisionTimeoutSeconds, nc.MaxRevisionTimeoutSeconds)
	}
	if nc.ContainerConcurrencyMaxLimit < 1 {
		return nil, apis.ErrOutOfBoundsValue(
			nc.ContainerConcurrencyMaxLimit, 1, math.MaxInt32, "container-concurrency-max-limit")
	}
	if nc.ContainerConcurrency < 0 || nc.ContainerConcurrency > nc.ContainerConcurrencyMaxLimit {
		return nil, apis.ErrOutOfBoundsValue(
			nc.ContainerConcurrency, 0, nc.ContainerConcurrencyMaxLimit, "container-concurrency")
	}

	tmpl, err := template.New("user-container").Parse(nc.UserContainerNameTemplate)
	if err != nil {
		return nil, err
	}
	// Check that the template properly applies to ObjectMeta.
	if err := tmpl.Execute(ioutil.Discard, metav1.ObjectMeta{}); err != nil {
		return nil, fmt.Errorf("error executing template: %w", err)
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

	UserContainerNameTemplate string

	ContainerConcurrency int64

	// ContainerConcurrencyMaxLimit is the maximum permitted container concurrency
	// or target value in the system.
	ContainerConcurrencyMaxLimit int64

	// AllowContainerConcurrencyZero determines whether users are permitted to specify
	// a containerConcurrency of 0 (i.e. unbounded).
	AllowContainerConcurrencyZero bool

	RevisionCPURequest    *resource.Quantity
	RevisionCPULimit      *resource.Quantity
	RevisionMemoryRequest *resource.Quantity
	RevisionMemoryLimit   *resource.Quantity
}

// UserContainerName returns the name of the user container based on the context.
func (d *Defaults) UserContainerName(ctx context.Context) string {
	tmpl := template.Must(
		template.New("user-container").Parse(d.UserContainerNameTemplate))
	buf := &bytes.Buffer{}
	if err := tmpl.Execute(buf, apis.ParentMeta(ctx)); err != nil {
		return ""
	}
	return buf.String()
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
