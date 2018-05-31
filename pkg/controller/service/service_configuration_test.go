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
	"testing"

	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testServiceName            string = "test-service"
	testServiceNamespace       string = "test-service-namespace"
	testRevisionName           string = "test-revision-name"
	testContainerNameRunLatest string = "test-container-run-latest"
	testContainerNamePinned    string = "test-container-pinned"
	testLabelKey               string = "test-label-key"
	testLabelValuePinned       string = "test-label-value-pinned"
	testLabelValueRunLatest    string = "test-label-value-run-latest"
)

func createConfiguration(containerName string) v1alpha1.ConfigurationSpec {
	return v1alpha1.ConfigurationSpec{
		RevisionTemplate: v1alpha1.RevisionTemplateSpec{
			Spec: v1alpha1.RevisionSpec{
				Container: corev1.Container{
					Name: containerName,
				},
			},
		},
	}
}

func createServiceMeta() *v1alpha1.Service {
	return &v1alpha1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testServiceName,
			Namespace: testServiceNamespace,
		},
	}
}

func createServiceWithRunLatest() *v1alpha1.Service {
	s := createServiceMeta()
	s.Spec = v1alpha1.ServiceSpec{
		RunLatest: &v1alpha1.RunLatestType{
			Configuration: createConfiguration(testContainerNameRunLatest),
		},
	}
	s.Labels = make(map[string]string, 2)
	s.Labels[testLabelKey] = testLabelValueRunLatest
	return s
}

func createServiceWithPinned() *v1alpha1.Service {
	s := createServiceMeta()
	s.Spec = v1alpha1.ServiceSpec{
		Pinned: &v1alpha1.PinnedType{
			RevisionName:  testRevisionName,
			Configuration: createConfiguration(testContainerNamePinned),
		},
	}
	s.Labels = make(map[string]string, 2)
	s.Labels[testLabelKey] = testLabelValuePinned
	return s
}

func TestRunLatest(t *testing.T) {
	s := createServiceWithRunLatest()
	c := MakeServiceConfiguration(s)
	if got, want := c.Name, testServiceName; got != want {
		t.Errorf("expected %q for service name got %q", want, got)
	}
	if got, want := c.Namespace, testServiceNamespace; got != want {
		t.Errorf("expected %q for service namespace got %q", want, got)
	}
	if got, want := c.Spec.RevisionTemplate.Spec.Container.Name, testContainerNameRunLatest; got != want {
		t.Errorf("expected %q for container name got %q", want, got)
	}
	expectOwnerReferencesSetCorrectly(t, c.OwnerReferences)

	if got, want := len(c.Labels), 2; got != want {
		t.Errorf("expected %d labels got %d", want, got)
	}
	if got, want := c.Labels[testLabelKey], testLabelValueRunLatest; got != want {
		t.Errorf("expected %q labels got %q", want, got)
	}
	if got, want := c.Labels[serving.ServiceLabelKey], testServiceName; got != want {
		t.Errorf("expected %q labels got %q", want, got)
	}
}

func TestPinned(t *testing.T) {
	s := createServiceWithPinned()
	c := MakeServiceConfiguration(s)
	if got, want := c.Name, testServiceName; got != want {
		t.Errorf("expected %q for service name got %q", want, got)
	}
	if got, want := c.Namespace, testServiceNamespace; got != want {
		t.Errorf("expected %q for service namespace got %q", want, got)
	}
	if got, want := c.Spec.RevisionTemplate.Spec.Container.Name, testContainerNamePinned; got != want {
		t.Errorf("expected %q for container name got %q", want, got)
	}
	expectOwnerReferencesSetCorrectly(t, c.OwnerReferences)

	if got, want := len(c.Labels), 2; got != want {
		t.Errorf("expected %d labels got %d", want, got)
	}
	if got, want := c.Labels[testLabelKey], testLabelValuePinned; got != want {
		t.Errorf("expected %q labels got %q", want, got)
	}
	if got, want := c.Labels[serving.ServiceLabelKey], testServiceName; got != want {
		t.Errorf("expected %q labels got %q", want, got)
	}
}

func expectOwnerReferencesSetCorrectly(t *testing.T, ownerRefs []metav1.OwnerReference) {
	if got, want := len(ownerRefs), 1; got != want {
		t.Errorf("expected %d owner refs got %d", want, got)
		return
	}

	expectedRefs := []metav1.OwnerReference{
		metav1.OwnerReference{
			APIVersion: "knative.dev/v1alpha1",
			Kind:       "Service",
			Name:       testServiceName,
		},
	}
	if diff := cmp.Diff(expectedRefs, ownerRefs, cmpopts.IgnoreFields(expectedRefs[0], "Controller", "BlockOwnerDeletion")); diff != "" {
		t.Errorf("Unexpected service owner refs diff (-want +got): %v", diff)
	}
}
