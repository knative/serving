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
func (s *IngressSpec) SetDefaults(ctx context.Context) {
	for i := range s.TLS {
		s.TLS[i].SetDefaults(ctx)
	}
	for i := range s.Rules {
		s.Rules[i].SetDefaults(ctx)
	}

	// Deprecated, do not use.
	s.DeprecatedVisibility = ""
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
func (r *HTTPIngressRuleValue) SetDefaults(ctx context.Context) {
	for i := range r.Paths {
		r.Paths[i].SetDefaults(ctx)
	}
}

// SetDefaults populates default values in HTTPIngressPath
func (p *HTTPIngressPath) SetDefaults(ctx context.Context) {
	// If only one split is specified, we default to 100.
	if len(p.Splits) == 1 && p.Splits[0].Percent == 0 {
		p.Splits[0].Percent = 100
	}
	// Deprecated, do not use.
	p.DeprecatedRetries = nil
}
