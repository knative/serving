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

	"github.com/knative/pkg/apis"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/serving/pkg/apis/networking"
)

func (i *Ingress) SetDefaults(ctx context.Context) {
	i.Spec.SetDefaults(apis.WithinSpec(ctx))
}

func (s *IngressSpec) SetDefaults(ctx context.Context) {
	for i := range s.TLS {
		s.TLS[i].SetDefaults(ctx)
	}
	for i := range s.Rules {
		s.Rules[i].SetDefaults(ctx)
	}
	if s.Visibility == "" {
		s.Visibility = IngressVisibilityExternalIP
	}
}

func (t *IngressTLS) SetDefaults(ctx context.Context) {
	// Default Secret key for ServerCertificate is `tls.crt`.
	if t.ServerCertificate == "" {
		t.ServerCertificate = "tls.crt"
	}
	// Default Secret key for PrivateKey is `tls.key`.
	if t.PrivateKey == "" {
		t.PrivateKey = "tls.key"
	}
}

func (r *IngressRule) SetDefaults(ctx context.Context) {
	r.HTTP.SetDefaults(ctx)
}

func (r *HTTPIngressRuleValue) SetDefaults(ctx context.Context) {
	for i := range r.Paths {
		r.Paths[i].SetDefaults(ctx)
	}
}

func (p *HTTPIngressPath) SetDefaults(ctx context.Context) {
	// If only one split is specified, we default to 100.
	if len(p.Splits) == 1 && p.Splits[0].Percent == 0 {
		p.Splits[0].Percent = 100
	}

	if p.Timeout == nil {
		p.Timeout = &metav1.Duration{Duration: networking.DefaultTimeout}
	}

	if p.Retries == nil {
		p.Retries = &HTTPRetry{
			PerTryTimeout: &metav1.Duration{Duration: networking.DefaultTimeout},
			Attempts:      networking.DefaultRetryCount,
		}
	}
	if p.Retries.PerTryTimeout == nil {
		p.Retries.PerTryTimeout = &metav1.Duration{Duration: networking.DefaultTimeout}
	}
}
