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

package e2e

import (
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/knative/pkg/apis/duck"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	"github.com/knative/serving/test"
)

func TestV1beta1Translation(t *testing.T) {
	t.Parallel()
	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating a new Service")
	// Create a legacy RunLatest service.  This should perform conversion during the webhook
	// and return back a converted service resource.
	service, err := test.CreateLatestServiceLegacy(t, clients, names, &test.Options{})
	if err != nil {
		t.Fatalf("Failed to create initial Service: %v: %v", names.Service, err)
	}

	// Access the service over the v1beta1 endpoint using the dynamic client.
	gvr := v1beta1.SchemeGroupVersion.WithResource("services")
	svc, err := clients.Dynamic.Resource(gvr).Namespace(service.Namespace).
		Get(service.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get v1beta1.Service: %v: %v", names.Service, err)
	}

	// Translate it into a v1beta1 Service resource.
	v1b1 := &v1beta1.Service{}
	if err := duck.FromUnstructured(svc, v1b1); err != nil {
		t.Fatalf("Failed to parse unstructured as v1beta1.Service: %v: %v", names.Service, err)
	}

	// Check that the PodSpecs match
	if !equality.Semantic.DeepEqual(v1b1.Spec.Template.Spec.PodSpec, service.Spec.Template.Spec.PodSpec) {
		t.Fatalf("Failed to parse unstructured as v1beta1.Service: %v: %v", names.Service, err)
	}
}

func TestV1beta1Rejection(t *testing.T) {
	t.Parallel()
	clients := Setup(t)

	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   "helloworld",
	}

	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })
	defer test.TearDown(clients, names)

	t.Log("Creating a new Service")
	// Create a legacy RunLatest service, but give it the TypeMeta of v1beta1.
	service := test.LatestServiceLegacy(names, &test.Options{})
	service.APIVersion = v1beta1.SchemeGroupVersion.String()
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

	// Try to create the "run latest" service through v1beta1.
	gvr := v1beta1.SchemeGroupVersion.WithResource("services")
	svc, err := clients.Dynamic.Resource(gvr).Namespace(service.Namespace).
		Create(u, metav1.CreateOptions{})
	if err == nil {
		t.Fatalf("Unexpected success creating %#v", svc)
	}
}
