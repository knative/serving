// +build e2e

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

package v1

import (
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"knative.dev/pkg/test/logstream"
	v1a1test "knative.dev/serving/test/v1alpha1"

	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/test"
)

func TestTranslation(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating a new Service")
	// Create a legacy RunLatest service.  This should perform conversion during the webhook
	// and return back a converted service resource.
	service, err := v1a1test.CreateLatestServiceLegacy(t, clients, names)
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	// Access the service over the v1 endpoint.
	v1b1, err := clients.ServingClient.Services.Get(service.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get v1.Service: %v: %v", names.Service, err)
	}

	// Access the service over the v1 endpoint.
	v1, err := clients.ServingClient.Services.Get(service.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get v1.Service: %v: %v", names.Service, err)
	}

	// Check that all PodSpecs match
	if !equality.Semantic.DeepEqual(v1b1.Spec.Template.Spec.PodSpec, service.Spec.Template.Spec.PodSpec) {
		t.Fatalf("Failed to parse unstructured as v1.Service: %v: %v", names.Service, err)
	}
	if !equality.Semantic.DeepEqual(v1.Spec.Template.Spec.PodSpec, service.Spec.Template.Spec.PodSpec) {
		t.Fatalf("Failed to parse unstructured as v1.Service: %v: %v", names.Service, err)
	}
}

func TestV1beta1Rejection(t *testing.T) {
	t.Parallel()
	cancel := logstream.Start(t)
	defer cancel()

	clients := test.Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating a new Service")
	// Create a legacy RunLatest service, but give it the TypeMeta of v1.
	service := v1a1test.LatestServiceLegacy(names)
	service.APIVersion = v1.SchemeGroupVersion.String()
	service.Kind = "Service"

	// Turn it into an unstructured resource for sending through the dynamic client.
	b, err := json.Marshal(service)
	if err != nil {
		t.Fatalf("Failed to marshal v1alpha1.Service: %v: %v", names.Service, err)
	}
	u := &unstructured.Unstructured{}
	if err := json.Unmarshal(b, u); err != nil {
		t.Fatalf("Failed to unmarshal as unstructured: %v: %v", names.Service, err)
	}

	// Try to create the "run latest" service through v1.
	gvr := v1.SchemeGroupVersion.WithResource("services")
	svc, err := clients.Dynamic.Resource(gvr).Namespace(service.Namespace).
		Create(u, metav1.CreateOptions{})
	if err == nil {
		t.Fatalf("Unexpected success creating %#v", svc)
	}
}
