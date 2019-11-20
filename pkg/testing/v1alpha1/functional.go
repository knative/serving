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
	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/ptr"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
)

var (
	// configSpec is the spec used for the different styles of Service rollout.
	configSpec = v1alpha1.ConfigurationSpec{
		DeprecatedRevisionTemplate: &v1alpha1.RevisionTemplateSpec{
			Spec: v1alpha1.RevisionSpec{
				DeprecatedContainer: &corev1.Container{
					Image: "busybox",
				},
				RevisionSpec: v1.RevisionSpec{
					TimeoutSeconds: ptr.Int64(60),
				},
			},
		},
	}
)
