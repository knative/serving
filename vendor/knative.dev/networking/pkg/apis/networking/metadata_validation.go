/*
Copyright 2020 The Knative Authors

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

package networking

import (
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"

	"knative.dev/pkg/apis"
)

var (
	allowedAnnotations = sets.NewString(
		IngressClassAnnotationKey,
		CertificateClassAnnotationKey,
		DisableAutoTLSAnnotationKey,
		HTTPOptionAnnotationKey,

		IngressClassAnnotationAltKey,
		CertificateClassAnnotationAltKey,
		DisableAutoTLSAnnotationAltKey,
		HTTPProtocolAnnotationKey,
	)
)

// ValidateAnnotations validates that `annotations` in `metadata` stanza of the
// resources is correct.
func ValidateAnnotations(annotations map[string]string) (errs *apis.FieldError) {
	for key := range annotations {
		if !allowedAnnotations.Has(key) && strings.HasPrefix(key, "networking.knative.dev") {
			errs = errs.Also(apis.ErrInvalidKeyName(key, apis.CurrentField))
		}
	}
	return errs
}
