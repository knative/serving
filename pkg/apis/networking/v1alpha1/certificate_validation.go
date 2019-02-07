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
	"github.com/knative/pkg/apis"
	"k8s.io/apimachinery/pkg/api/equality"
)

// Validate inspects and validates Certificate object.
func (c *Certificate) Validate() *apis.FieldError {
	return c.Spec.Validate().ViaField("spec")
}

// Validate inspects and validates CertificateSpec object.
func (spec *CertificateSpec) Validate() *apis.FieldError {
	// Spec must not be empty.
	if equality.Semantic.DeepEqual(spec, &CertificateSpec{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}
	var all *apis.FieldError
	// Spec must have at least one DNS Name.
	if len(spec.DNSNames) == 0 {
		all = all.Also(apis.ErrMissingField("dnsNames"))
	} else {
		for index, dnsName := range spec.DNSNames {
			if len(dnsName) == 0 {
				all = all.Also(apis.ErrInvalidArrayValue("DNS Name cannot be empty string.", "dnsNames", index))
			}
		}
	}

	// Spec must have secretName.
	if len(spec.SecretName) == 0 {
		all = all.Also(apis.ErrMissingField("secretName"))
	}
	return all
}
