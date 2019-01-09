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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"

	"github.com/knative/pkg/apis"
)

func (rt *Image) Validate() *apis.FieldError {
	return rt.Spec.Validate().ViaField("spec")
}

func (rs *ImageSpec) Validate() *apis.FieldError {
	if rs.Image == "" {
		return apis.ErrMissingField("image")
	}
	// TODO(mattmoor): Consider using go-containerregistry to validate
	// the image reference.  This is effectively the function we want.
	// https://github.com/google/go-containerregistry/blob/2f3e3e1/pkg/name/ref.go#L41
	for index, ips := range rs.ImagePullSecrets {
		if equality.Semantic.DeepEqual(ips, corev1.LocalObjectReference{}) {
			return apis.ErrMissingField(fmt.Sprintf("imagePullSecrets[%d].name", index))
		}
	}
	return nil
}
