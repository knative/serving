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

func (spec *IngressSpec) Validate() *apis.FieldError {
	// Spec must not be empty.
	if equality.Semantic.DeepEqual(spec, &IngressSpec{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	var all *apis.FieldError = nil
	// Spec must have at least one rule.
	if len(spec.Rules) == 0 {
		all = all.Also(apis.ErrMissingField("rules"))
	}
	// Validate each rule.
	for idx, rule := range spec.Rules {
		all = all.Also(rule.Validate().ViaFieldIndex("rules", idx))
	}
	// TLS settings are optional.  However, all provided settings should be valid.
	for idx, tls := range spec.TLS {
		all = all.Also(tls.Validate().ViaFieldIndex("tls", idx))
	}
	return all
}

func (r *ClusterIngressRule) Validate() *apis.FieldError {
	// Provided rule must not be empty.
	if equality.Semantic.DeepEqual(r, &ClusterIngressRule{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	var all *apis.FieldError = nil
	if r.HTTP == nil {
		all = all.Also(apis.ErrMissingField("http"))
	} else {
		all = all.Also(r.HTTP.Validate().ViaField("http"))
	}
	return all
}

func (h *HTTPClusterIngressRuleValue) Validate() *apis.FieldError {
	if len(h.Paths) == 0 {
		return apis.ErrMissingField("paths")
	}
	var all *apis.FieldError = nil
	for idx, path := range h.Paths {
		all = all.Also(path.Validate().ViaFieldIndex("paths", idx))
	}
	return all
}

func (h HTTPClusterIngressPath) Validate() *apis.FieldError {
	// Provided rule must not be empty.
	if equality.Semantic.DeepEqual(h, HTTPClusterIngressPath{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	var all *apis.FieldError = nil
	// Must provide as least one split.
	if len(h.Splits) == 0 {
		all = all.Also(apis.ErrMissingField("splits"))
	} else {
		totalPct := 0
		for idx, split := range h.Splits {
			if err := split.Validate(); err != nil {
				return err.ViaFieldIndex("splits", idx)
			}
			totalPct += split.Percent
		}
		// If a single split is provided we allow missing Percent, and
		// interpret as 100%.
		if len(h.Splits) == 1 && totalPct == 0 {
			totalPct = 100
		}
		// Total traffic split percentage must sum up to 100%.
		if totalPct != 100 {
			all = all.Also(&apis.FieldError{
				Message: "Traffic split percentage must total to 100",
				Paths:   []string{"splits"},
			})
		}
	}
	if h.Retries != nil {
		all = all.Also(h.Retries.Validate().ViaField("retries"))
	}
	return all
}

func (s ClusterIngressBackendSplit) Validate() *apis.FieldError {
	// Must not be empty.
	if equality.Semantic.DeepEqual(s, ClusterIngressBackendSplit{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	var all *apis.FieldError = nil
	// Percent must be between 0 and 100.
	if s.Percent < 0 || s.Percent > 100 {
		all = all.Also(apis.ErrInvalidValue(fmt.Sprintf("%d", s.Percent), "percent"))
	}
	return all.Also(s.ClusterIngressBackend.Validate())
}

// Validate inspects the fields of the type ClusterIngressBackend
// to determine if they are valid.
func (b ClusterIngressBackend) Validate() *apis.FieldError {
	// Must not be empty.
	if equality.Semantic.DeepEqual(b, ClusterIngressBackend{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	var all *apis.FieldError = nil
	if b.ServiceNamespace == "" {
		all = all.Also(apis.ErrMissingField("serviceNamespace"))
	}
	if b.ServiceName == "" {
		all = all.Also(apis.ErrMissingField("serviceName"))
	}
	if equality.Semantic.DeepEqual(b.ServicePort, intstr.IntOrString{}) {
		all = all.Also(apis.ErrMissingField("servicePort"))
	}
	return all
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
	var all *apis.FieldError = nil
	// SecretName and SecretNamespace must not be empty.
	if t.SecretName == "" {
		all = all.Also(apis.ErrMissingField("secretName"))
	}
	if t.SecretNamespace == "" {
		all = all.Also(apis.ErrMissingField("secretNamespace"))
	}
	return all
}
