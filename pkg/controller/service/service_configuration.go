/*
Copyright 2018 Google LLC

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

package service

import (
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	"github.com/elafros/elafros/pkg/controller"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MakeServiceConfiguration creates a Configuration from a Service object.
func MakeServiceConfiguration(service *v1alpha1.Service) *v1alpha1.Configuration {
	c := &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      service.Name,
			Namespace: service.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*controller.NewServiceControllerRef(service),
			},
			Labels: MakeElaResourceLabels(service),
		},
	}

	if service.Spec.RunLatest != nil {
		c.Spec = service.Spec.RunLatest.Configuration
	} else {
		c.Spec = service.Spec.Pinned.Configuration
	}
	return c
}
