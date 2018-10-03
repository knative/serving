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

package v1alpha1

import (
	"fmt"

	"github.com/knative/pkg/apis"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func (ci *ClusterIngress) Validate() *apis.FieldError {
	return ci.Spec.Validate().ViaField("spec")
}

func (spec *ClusterIngressSpec) Validate() *apis.FieldError {
	fmt.Printf("%#v\n", spec)
	// Spec must not be empty.
	if equality.Semantic.DeepEqual(spec, &ClusterIngressSpec{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	// Spec must have at least one rule.
	if len(spec.Rules) == 0 {
		return apis.ErrMissingField("rules")
	}
	// Validate each rule.
	for idx, rule := range spec.Rules {
		if err := rule.Validate(); err != nil {
			return err.ViaField(fmt.Sprintf("rules[%d]", idx))
		}
	}
	// TLS settings are optional.  However, all provided settings should be valid.
	for idx, tls := range spec.TLS {
		if err := tls.Validate(); err != nil {
			return err.ViaField(fmt.Sprintf("tls[%d]", idx))
		}
	}
	return nil
}

func (r *ClusterIngressRule) Validate() *apis.FieldError {
	// Provided rule must not be empty.
	if equality.Semantic.DeepEqual(r, &ClusterIngressRule{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	if len(r.Hosts) == 0 {
		return apis.ErrMissingField("hosts")
	}
	if err := validateRuleValue(r.ClusterIngressRuleValue); err != nil {
		return err
	}
	return nil
}

func validateRuleValue(r ClusterIngressRuleValue) *apis.FieldError {
	// At least one kind of ClusterIngressRuleValue is required.
	// Since we only have HTTPClusterIngressRuleValue now, it must be provided.
	if r.HTTP == nil {
		return apis.ErrMissingField("http")
	}
	return r.HTTP.Validate().ViaField("http")
}

func (h *HTTPClusterIngressRuleValue) Validate() *apis.FieldError {
	if len(h.Paths) == 0 {
		return apis.ErrMissingField("paths")
	}
	for idx, path := range h.Paths {
		if err := path.Validate(); err != nil {
			return err.ViaField(fmt.Sprintf("paths[%d]", idx))
		}
	}
	return nil
}

func (s ClusterIngressBackendSplit) Validate() *apis.FieldError {
	// Must not be empty.
	if equality.Semantic.DeepEqual(s, ClusterIngressBackendSplit{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	// Percent must be between 0 and 100.
	if s.Percent < 0 || s.Percent > 100 {
		return apis.ErrInvalidValue(fmt.Sprintf("%d", s.Percent), "percent")
	}
	return s.Backend.Validate().ViaField("backend")
}

func (b *ClusterIngressBackend) Validate() *apis.FieldError {
	// Must not be empty.
	if equality.Semantic.DeepEqual(b, &ClusterIngressBackend{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	if b.ServiceNamespace == "" {
		return apis.ErrMissingField("serviceNamespace")
	}
	if b.ServiceName == "" {
		return apis.ErrMissingField("serviceName")
	}
	if equality.Semantic.DeepEqual(b.ServicePort, intstr.IntOrString{}) {
		return apis.ErrMissingField("servicePort")
	}
	return nil
}

func (h HTTPClusterIngressPath) Validate() *apis.FieldError {
	// Provided rule must not be empty.
	if equality.Semantic.DeepEqual(h, HTTPClusterIngressPath{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	// Must provide as least one split.
	if len(h.Splits) == 0 {
		return apis.ErrMissingField("splits")
	}
	totalPct := 0
	for idx, split := range h.Splits {
		if err := split.Validate(); err != nil {
			return err.ViaField(fmt.Sprintf("splits[%d]", idx))
		}
		pct := split.Percent
		// If a single split is provided we allow missing Percent, and
		// interpret as 100.
		if len(h.Splits) == 1 && pct == 0 {
			pct = 100
		}
		totalPct += pct
	}
	// Total traffic split percentage must sum up to 100%.
	if totalPct != 100 {
		return &apis.FieldError{
			Message: "Traffic split percentage must total to 100",
			Paths:   []string{"splits"},
		}
	}
	if h.Retries != nil {
		if err := h.Retries.Validate(); err != nil {
			return err.ViaField("retries")
		}
	}
	return nil
}

func (r *HTTPRetry) Validate() *apis.FieldError {
	// Attempts must be greater than 0.
	if r.Attempts < 0 {
		return apis.ErrInvalidValue(fmt.Sprintf("%d", r.Attempts), "attempts")
	}
	return nil
}

func (t *ClusterIngressTLS) Validate() *apis.FieldError {
	// Provided TLS setting must not be empty.
	if equality.Semantic.DeepEqual(t, &ClusterIngressTLS{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	// SecretName and SecretNamespace must not be empty.
	if t.SecretName == "" {
		return apis.ErrMissingField("secretName")
	}
	if t.SecretNamespace == "" {
		return apis.ErrMissingField("secretNamespace")
	}
	return nil
}
