/*
Copyright 2021 The Knative Authors

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
)

// KReference creates a KReference which points to KService.
func KReference(namespace, name string) *duckv1.KReference {
	return &duckv1.KReference{
		Namespace:  namespace,
		Name:       name,
		APIVersion: "serving.knative.dev/v1",
		Kind:       "Service",
	}
}

// DomainMapping creates a domainmapping with KReference.
func DomainMapping(namespace, name string, kref *duckv1.KReference) *v1alpha1.DomainMapping {
	dm := &v1alpha1.DomainMapping{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: v1alpha1.DomainMappingSpec{
			Ref: *kref,
		},
	}
	return dm
}
