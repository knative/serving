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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MakeServiceRoute creates a Route from a Service object.
func MakeServiceRoute(service *v1alpha1.Service, configName string, revisionName string) *v1alpha1.Route {
	c := &v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      service.Name,
			Namespace: service.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(service, controllerKind),
			},
		},
	}

	tt := v1alpha1.TrafficTarget{
		Percent:           100,
		ConfigurationName: configName,
	}
	if len(revisionName) != 0 {
		tt.RevisionName = revisionName
	}
	c.Spec.Traffic = append(c.Spec.Traffic, tt)
	return c
}
