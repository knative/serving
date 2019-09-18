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

package v1

import (
	corev1 "k8s.io/api/core/v1"

	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

// ConfigOption enables further configuration of a Configuration.
type ConfigOption func(*v1.Configuration)

// WithConfigReadinessProbe sets the provided probe to be the readiness
// probe on the configuration.
func WithConfigReadinessProbe(p *corev1.Probe) ConfigOption {
	return func(cfg *v1.Configuration) {
		cfg.Spec.Template.Spec.Containers[0].ReadinessProbe = p
	}
}

// WithConfigImage sets the container image to be the provided string.
func WithConfigImage(img string) ConfigOption {
	return func(cfg *v1.Configuration) {
		cfg.Spec.Template.Spec.Containers[0].Image = img
	}
}
