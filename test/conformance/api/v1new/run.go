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
package v1new

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

func RunConformance(t *ConformanceT) {
	t.Run("Service", func(t *ConformanceT) {
		t.Stable("single container", TestService)
		t.Alpha("multi container", TestServiceMultiContainer)
	})
}

func TestService(t *ConformanceT) {
	t.Parallel()

	names := &ResourceNames{
		Service: t.ObjectNameForTest(),
	}

	// TODO - we can do this automatically
	t.CleanupResources(names)

	svc := &v1.Service{}
	svc.Name = names.Service
	svc.Spec.Template.Spec.Containers = []corev1.Container{
		{Image: t.Images.Path("helloworld")},
	}

	_, err := t.ServingClients.Services.Create(context.TODO(), svc, metav1.CreateOptions{})
	if err != nil {
		t.Error(err)
	}

	if err = WaitForReadyService(t.ServingClients, names.Service); err != nil {
		t.Error(err)
	}
}

func TestServiceMultiContainer(t *ConformanceT) {
	t.Parallel()

	names := &ResourceNames{
		Service: t.ObjectNameForTest(),
	}

	t.CleanupResources(names)

	svc := &v1.Service{}
	svc.Name = names.Service
	svc.Spec.Template.Spec.Containers = []corev1.Container{
		{
			Image: t.Images.Path("servingcontainer"),
			Ports: []corev1.ContainerPort{{ContainerPort: 8881}},
		},
		{Image: t.Images.Path("sidecarcontainer")},
	}

	_, err := t.ServingClients.Services.Create(context.TODO(), svc, metav1.CreateOptions{})
	if err != nil {
		t.Error(err)
	}

	if err = WaitForReadyService(t.ServingClients, names.Service); err != nil {
		t.Error(err)
	}
}
