/*
Copyright 2019 The Knative Authors

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

package config

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/pkg/apis"
	cm "knative.dev/pkg/configmap"
	"knative.dev/pkg/ptr"
)

const (
	// DefaultsConfigName is the name of config map for the defaults.
	DefaultsConfigName = "config-defaults"

	// DefaultRevisionTimeoutSeconds will be set if timeoutSeconds not specified.
	DefaultRevisionTimeoutSeconds = 5 * 60

	// DefaultMaxRevisionTimeoutSeconds will be set if MaxRevisionTimeoutSeconds is not specified.
	DefaultMaxRevisionTimeoutSeconds = 10 * 60

	// DefaultInitContainerName is the default name we give to the init containers
	// specified by the user, if `name:` is omitted.
	DefaultInitContainerName = "init-container"

	// DefaultUserContainerName is the default name we give to the container
	// specified by the user, if `name:` is omitted.
	DefaultUserContainerName = "user-container"

	// DefaultContainerConcurrency is the default container concurrency. It will be set if ContainerConcurrency is not specified.
	DefaultContainerConcurrency = 0

	// DefaultMaxRevisionContainerConcurrency is the maximum configurable
	// container concurrency.
	DefaultMaxRevisionContainerConcurrency = 1000

	// DefaultAllowContainerConcurrencyZero is whether, by default,
	// containerConcurrency can be set to zero (i.e. unbounded) by users.
	DefaultAllowContainerConcurrencyZero = true
)

var (
	DefaultInitContainerNameTemplate = mustParseTemplate(DefaultInitContainerName)
	DefaultUserContainerNameTemplate = mustParseTemplate(DefaultUserContainerName)
)

func defaultDefaultsConfig() *Defaults {
	return &Defaults{
		RevisionTimeoutSeconds:        DefaultRevisionTimeoutSeconds,
		MaxRevisionTimeoutSeconds:     DefaultMaxRevisionTimeoutSeconds,
		InitContainerNameTemplate:     DefaultInitContainerNameTemplate,
		UserContainerNameTemplate:     DefaultUserContainerNameTemplate,
		ContainerConcurrency:          DefaultContainerConcurrency,
		ContainerConcurrencyMaxLimit:  DefaultMaxRevisionContainerConcurrency,
		AllowContainerConcurrencyZero: DefaultAllowContainerConcurrencyZero,
		EnableServiceLinks:            ptr.Bool(false),
	}
}

func asTriState(key string, target **bool, defValue *bool) cm.ParseFunc {
	return func(data map[string]string) error {
		if raw, ok := data[key]; ok {
			switch {
			case strings.EqualFold(raw, "true"):
				*target = ptr.Bool(true)
			case strings.EqualFold(raw, "false"):
				*target = ptr.Bool(false)
			default:
				*target = defValue
			}
		}
		return nil
	}
}

// NewDefaultsConfigFromMap creates a Defaults from the supplied Map.
func NewDefaultsConfigFromMap(data map[string]string) (*Defaults, error) {
	nc := defaultDefaultsConfig()

	if err := cm.Parse(data,
		asTemplate("init-container-name-template", &nc.InitContainerNameTemplate),
		asTemplate("container-name-template", &nc.UserContainerNameTemplate),

		cm.AsBool("allow-container-concurrency-zero", &nc.AllowContainerConcurrencyZero),
		asTriState("enable-service-links", &nc.EnableServiceLinks, nil),

		cm.AsInt64("revision-timeout-seconds", &nc.RevisionTimeoutSeconds),
		cm.AsInt64("max-revision-timeout-seconds", &nc.MaxRevisionTimeoutSeconds),
		cm.AsInt64("container-concurrency", &nc.ContainerConcurrency),
		cm.AsInt64("container-concurrency-max-limit", &nc.ContainerConcurrencyMaxLimit),

		cm.AsQuantity("revision-cpu-request", &nc.RevisionCPURequest),
		cm.AsQuantity("revision-memory-request", &nc.RevisionMemoryRequest),
		cm.AsQuantity("revision-ephemeral-storage-request", &nc.RevisionEphemeralStorageRequest),
		cm.AsQuantity("revision-cpu-limit", &nc.RevisionCPULimit),
		cm.AsQuantity("revision-memory-limit", &nc.RevisionMemoryLimit),
		cm.AsQuantity("revision-ephemeral-storage-limit", &nc.RevisionEphemeralStorageLimit),
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
	// Check that the templates properly apply to ObjectMeta.
	if err := nc.UserContainerNameTemplate.Execute(io.Discard, metav1.ObjectMeta{}); err != nil {
		return nil, fmt.Errorf("error executing template: %w", err)
	}
	if err := nc.InitContainerNameTemplate.Execute(io.Discard, metav1.ObjectMeta{}); err != nil {
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

	InitContainerNameTemplate *ObjectMetaTemplate

	UserContainerNameTemplate *ObjectMetaTemplate

	ContainerConcurrency int64

	// ContainerConcurrencyMaxLimit is the maximum permitted container concurrency
	// or target value in the system.
	ContainerConcurrencyMaxLimit int64

	// AllowContainerConcurrencyZero determines whether users are permitted to specify
	// a containerConcurrency of 0 (i.e. unbounded).
	AllowContainerConcurrencyZero bool

	// Permits defaulting of `enableServiceLinks` pod spec field.
	// See: https://github.com/knative/serving/issues/8498 for details.
	EnableServiceLinks *bool

	RevisionCPURequest              *resource.Quantity
	RevisionCPULimit                *resource.Quantity
	RevisionMemoryRequest           *resource.Quantity
	RevisionMemoryLimit             *resource.Quantity
	RevisionEphemeralStorageRequest *resource.Quantity
	RevisionEphemeralStorageLimit   *resource.Quantity
}

func containerNameFromTemplate(ctx context.Context, tmpl *ObjectMetaTemplate) string {
	buf := &bytes.Buffer{}
	if err := tmpl.Execute(buf, apis.ParentMeta(ctx)); err != nil {
		return ""
	}
	return buf.String()
}

// UserContainerName returns the name of the user container based on the context.
func (d Defaults) UserContainerName(ctx context.Context) string {
	return containerNameFromTemplate(ctx, d.UserContainerNameTemplate)
}

// InitContainerName returns the name of the init container based on the context.
func (d Defaults) InitContainerName(ctx context.Context) string {
	return containerNameFromTemplate(ctx, d.InitContainerNameTemplate)
}

func asTemplate(key string, target **ObjectMetaTemplate) cm.ParseFunc {
	return func(data map[string]string) error {
		if raw, ok := data[key]; ok {
			val, err := template.New("container-template").Parse(raw)
			if err != nil {
				return err
			}

			*target = &ObjectMetaTemplate{val}
		}

		return nil
	}
}

// ObjectMetaTemplate is a template.Template wrapper for templates
// which can be applied to ObjectMeta. It implements DeepCopy and
// Equal so it can be used in Config structs and with go-cmp in
// tests.
type ObjectMetaTemplate struct {
	*template.Template
}

func mustParseTemplate(t string) *ObjectMetaTemplate {
	return &ObjectMetaTemplate{
		Template: template.Must(template.New("container-template").Parse(t)),
	}
}

func (t ObjectMetaTemplate) DeepCopy() ObjectMetaTemplate {
	var err error
	t.Template, err = t.Template.Clone()
	if err != nil {
		// Can't fail since the template is validated.
		panic(err)
	}

	return t
}

// Equal compares two ObjectMetaTemplates for equality, it is
// implemented so that tests using go-cmp work nicely. Equality is
// defined to mean that applying the template results in the same
// value given the same ObjectMeta.Name.
func (t ObjectMetaTemplate) Equal(t2 ObjectMetaTemplate) bool {
	var a, b bytes.Buffer
	if err := t.Execute(&a, metav1.ObjectMeta{Name: "abc"}); err != nil {
		return false
	}
	if err := t2.Execute(&b, metav1.ObjectMeta{Name: "abc"}); err != nil {
		return false
	}
	return a.String() == b.String()
}
