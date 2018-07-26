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

package webhook

import (
	"fmt"
	"testing"

	"github.com/knative/pkg/apis"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	. "github.com/knative/serving/pkg/logging/testing"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidConfigurationAllowed(t *testing.T) {
	configuration := createConfiguration(testGeneration, testConfigurationName)

	if err := Validate(TestContextWithLogger(t))(nil, &configuration, &configuration); err != nil {
		t.Fatalf("Expected allowed. Failed with %s", err)
	}
}

func TestEmptySpecInConfigurationNotAllowed(t *testing.T) {
	configuration := v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testConfigurationName,
		},
		Spec: v1alpha1.ConfigurationSpec{},
	}

	got := Validate(TestContextWithLogger(t))(nil, &configuration, &configuration)
	want := &apis.FieldError{
		Message: "missing field(s)",
		Paths:   []string{"spec"},
	}
	if got.Error() != want.Error() {
		t.Errorf("Validate() = %v, wanted %v", got, want)
	}
}

func TestEmptyTemplateInSpecNotAllowed(t *testing.T) {
	configuration := v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testConfigurationName,
		},
		Spec: v1alpha1.ConfigurationSpec{
			Generation:       testGeneration,
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{},
		},
	}

	got := Validate(TestContextWithLogger(t))(nil, &configuration, &configuration)
	want := &apis.FieldError{
		Message: "missing field(s)",
		Paths:   []string{"spec.revisionTemplate.spec"},
	}
	if got.Error() != want.Error() {
		t.Errorf("Validate() = %v, wanted %v", got, want)
	}
}

func TestEmptyContainerNotAllowed(t *testing.T) {
	configuration := v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testConfigurationName,
		},
		Spec: v1alpha1.ConfigurationSpec{
			Generation: testGeneration,
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					ServiceAccountName: "Fred",
				},
			},
		},
	}

	got := Validate(TestContextWithLogger(t))(nil, &configuration, &configuration)
	want := &apis.FieldError{
		Message: "missing field(s)",
		Paths:   []string{"spec.revisionTemplate.spec.container"},
	}
	if got.Error() != want.Error() {
		t.Errorf("Validate() = %v, wanted %v", got, want)
	}
}

func TestServingStateNotAllowed(t *testing.T) {
	container := corev1.Container{
		Name: "test",
	}
	configuration := v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testConfigurationName,
		},
		Spec: v1alpha1.ConfigurationSpec{
			Generation: testGeneration,
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					ServingState: v1alpha1.RevisionServingStateActive,
					Container:    container,
				},
			},
		},
	}
	expected := fmt.Sprintf("must not set the field(s): spec.revisionTemplate.spec.servingState")
	if err := Validate(TestContextWithLogger(t))(nil, &configuration, &configuration); err == nil || err.Error() != expected {
		t.Fatalf("Result of Validate function: %s. Expected: %s.", err, expected)
	}
}

func TestUnwantedFieldInContainerNotAllowed(t *testing.T) {
	container := corev1.Container{
		Name: "Not Allowed",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceName("cpu"): resource.MustParse("25m"),
			},
		},
		Ports: []corev1.ContainerPort{{
			Name:          "http",
			ContainerPort: 8080,
		}},
		VolumeMounts: []corev1.VolumeMount{{
			MountPath: "mount/path",
			Name:      "name",
		}},
		Lifecycle: &corev1.Lifecycle{},
	}
	configuration := v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testConfigurationName,
		},
		Spec: v1alpha1.ConfigurationSpec{
			Generation: testGeneration,
			RevisionTemplate: v1alpha1.RevisionTemplateSpec{
				Spec: v1alpha1.RevisionSpec{
					Container: container,
				},
			},
		},
	}
	want := &apis.FieldError{
		Message: "must not set the field(s)",
		Paths: []string{
			"spec.revisionTemplate.spec.container.name",
			"spec.revisionTemplate.spec.container.resources",
			"spec.revisionTemplate.spec.container.ports",
			"spec.revisionTemplate.spec.container.volumeMounts",
			"spec.revisionTemplate.spec.container.lifecycle",
		},
	}
	expected := want.Error()
	if err := Validate(TestContextWithLogger(t))(nil, &configuration, &configuration); err == nil || err.Error() != expected {
		t.Fatalf("Expected: %s. Failed with %s", expected, err)
	}
	configuration.Spec.RevisionTemplate.Spec.Container.Name = ""
	want.Paths = want.Paths[1:]
	expected = want.Error()
	if err := Validate(TestContextWithLogger(t))(nil, &configuration, &configuration); err == nil || err.Error() != expected {
		t.Fatalf("Expected: %s. Failed with %s", expected, err)
	}
	configuration.Spec.RevisionTemplate.Spec.Container.Resources = corev1.ResourceRequirements{}
	want.Paths = want.Paths[1:]
	expected = want.Error()
	if err := Validate(TestContextWithLogger(t))(nil, &configuration, &configuration); err == nil || err.Error() != expected {
		t.Fatalf("Expected: %s. Failed with %s", expected, err)
	}
}
