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
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
)

type domainMappingOption func(dm *v1alpha1.DomainMapping)

// WithRef fills reference field in domainmapping.
func WithRef(namespace, name string) domainMappingOption {
	return func(dm *v1alpha1.DomainMapping) {
		dm.Spec.Ref.Namespace = namespace
		dm.Spec.Ref.Name = name
		dm.Spec.Ref.APIVersion = "serving.knative.dev/v1"
		dm.Spec.Ref.Kind = "Service"
	}
}

// DomainMapping creates a domainmapping with domainMappingOption.
func DomainMapping(namespace, name string, opt ...domainMappingOption) *v1alpha1.DomainMapping {
	dm := &v1alpha1.DomainMapping{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}
	for _, o := range opt {
		o(dm)
	}
	return dm
}
