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
	"strings"

	"knative.dev/pkg/apis"
)

// Validate inspects and validates Certificate object.
func (c *Certificate) Validate(ctx context.Context) *apis.FieldError {
	return c.Spec.Validate(apis.WithinSpec(ctx)).ViaField("spec")
}

// Validate inspects and validates CertificateSpec object.
func (spec *CertificateSpec) Validate(ctx context.Context) (all *apis.FieldError) {
	// Spec must have at least one DNS Name.
	if len(spec.DNSNames) == 0 {
		all = all.Also(apis.ErrMissingField("dnsNames"))
	} else {
		for i, dnsName := range spec.DNSNames {
			if dnsName == "" {
				all = all.Also(apis.ErrInvalidArrayValue(dnsName, "dnsNames", i))
			}
		}
	}

	// Spec must have secretName.
	if spec.SecretName == "" {
		all = all.Also(apis.ErrMissingField("secretName"))
	}

	if spec.Domain != "" && len(spec.DNSNames) != 0 {
		suffix := "." + spec.Domain
		valid := false
		for _, dnsName := range spec.DNSNames {
			if strings.HasSuffix(dnsName, suffix) || dnsName == spec.Domain {
				valid = true
				break
			}
		}

		if !valid {
			all = all.Also(apis.ErrInvalidValue("domain", "domain must be a suffix of, or match exactly, at least one entry in dnsNames"))
		}
	}

	return all
}
