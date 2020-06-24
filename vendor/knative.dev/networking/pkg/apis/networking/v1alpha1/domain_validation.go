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

package v1alpha1

import (
	"context"

	"k8s.io/apimachinery/pkg/api/equality"
	"knative.dev/pkg/apis"
)

// Validate inspects and validates Domain object.
func (d *Domain) Validate(ctx context.Context) *apis.FieldError {
	return d.Spec.Validate(apis.WithinSpec(ctx)).ViaField("spec")
}

// Validate inspects and validates DomainSpec object.
func (spec *DomainSpec) Validate(ctx context.Context) *apis.FieldError {
	// Spec must not be empty.
	if equality.Semantic.DeepEqual(spec, &DomainSpec{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	var all *apis.FieldError
	if spec.IngressClass == "" {
		all = all.Also(apis.ErrMissingField("ingressClass"))
	}
	if len(spec.LoadBalancers) == 0 {
		all = all.Also(apis.ErrMissingField("loadBalancers"))
	}
	for idx, lbSpec := range spec.LoadBalancers {
		all = all.Also(lbSpec.Validate(ctx).ViaFieldIndex("loadBalancers", idx))
	}
	for idx, cfg := range spec.Configs {
		all = all.Also(cfg.Validate(ctx).ViaFieldIndex("configs", idx))
	}
	return all
}

func (lb *LoadBalancerIngressSpec) Validate(ctx context.Context) *apis.FieldError {
	var all *apis.FieldError
	if lb.Domain == "" && lb.DomainInternal == "" && lb.IP == "" && !lb.MeshOnly {
		return all.Also(apis.ErrMissingOneOf("domain", "domainInternal", "ip", "meshOnly"))
	}
	return all
}

func (cfg *IngressConfig) Validate(ctx context.Context) *apis.FieldError {
	var all *apis.FieldError
	if cfg.Name == "" {
		all = all.Also(apis.ErrMissingField("name"))
	}
	if cfg.Type == "" {
		all = all.Also(apis.ErrMissingField("type"))
	}
	return all
}
