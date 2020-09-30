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

package v1alpha1

import (
	"context"

	"knative.dev/pkg/apis"
)

// SetDefaults populates default values in Ingress
func (i *Ingress) SetDefaults(ctx context.Context) {
	i.Spec.SetDefaults(apis.WithinSpec(ctx))
}

// SetDefaults populates default values in IngressSpec
func (is *IngressSpec) SetDefaults(ctx context.Context) {
	for i := range is.TLS {
		is.TLS[i].SetDefaults(ctx)
	}
	for i := range is.Rules {
		is.Rules[i].SetDefaults(ctx)
	}

	// Deprecated, do not use.
	is.DeprecatedVisibility = ""
}

// SetDefaults populates default values in IngressTLS
func (t *IngressTLS) SetDefaults(ctx context.Context) {
	// Deprecated, do not use.
	t.DeprecatedServerCertificate = ""
	t.DeprecatedPrivateKey = ""
}

// SetDefaults populates default values in IngressRule
func (r *IngressRule) SetDefaults(ctx context.Context) {
	if r.Visibility == "" {
		r.Visibility = IngressVisibilityExternalIP
	}
	r.HTTP.SetDefaults(ctx)
}

// SetDefaults populates default values in HTTPIngressRuleValue
func (h *HTTPIngressRuleValue) SetDefaults(ctx context.Context) {
	for i := range h.Paths {
		h.Paths[i].SetDefaults(ctx)
	}
}

// SetDefaults populates default values in HTTPIngressPath
func (h *HTTPIngressPath) SetDefaults(ctx context.Context) {
	// If only one split is specified, we default to 100.
	if len(h.Splits) == 1 && h.Splits[0].Percent == 0 {
		h.Splits[0].Percent = 100
	}
	// Deprecated, do not use.
	h.DeprecatedRetries = nil
}
