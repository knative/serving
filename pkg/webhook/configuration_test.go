/*
Copyright 2018 The Knative Authors.
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
	"strings"
	"testing"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidConfigurationAllowed(t *testing.T) {
	configuration := createConfiguration(testGeneration, testConfigurationName)

	if err := ValidateConfiguration(testCtx)(nil, &configuration, &configuration); err != nil {
		t.Fatalf("Expected allowed. Failed with %s", err)
	}
}

func TestEmptyConfigurationNotAllowed(t *testing.T) {
	if err := ValidateConfiguration(testCtx)(nil, nil, nil); err != errInvalidConfigurationInput {
		t.Fatalf("Expected: %s. Failed with %s", errInvalidConfigurationInput, err)
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

	if err := ValidateConfiguration(testCtx)(nil, &configuration, &configuration); err != errEmptySpecInConfiguration {
		t.Fatalf("Expected: %s. Failed with %s", errEmptySpecInConfiguration, err)
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

	if err := ValidateConfiguration(testCtx)(nil, &configuration, &configuration); err != errEmptyRevisionTemplateInSpec {
		t.Fatalf("Expected: %s. Failed with %s", errEmptyRevisionTemplateInSpec, err)
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

	if err := ValidateConfiguration(testCtx)(nil, &configuration, &configuration); err != errEmptyContainerInRevisionTemplate {
		t.Fatalf("Expected: %v. Failed with %v", errEmptyRevisionTemplateInSpec, err)
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
	expected := fmt.Sprintf("The configuration spec must not set the field(s): revisionTemplate.spec.servingState")
	if err := ValidateConfiguration(testCtx)(nil, &configuration, &configuration); err == nil || err.Error() != expected {
		t.Fatalf("Result of ValidateConfiguration function: %s. Expected: %s.", err, expected)
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
	unwanted := []string{
		"revisionTemplate.spec.container.name",
		"revisionTemplate.spec.container.resources",
		"revisionTemplate.spec.container.ports",
		"revisionTemplate.spec.container.volumeMounts",
	}
	expected := fmt.Sprintf("The configuration spec must not set the field(s): %s", strings.Join(unwanted, ", "))
	if err := ValidateConfiguration(testCtx)(nil, &configuration, &configuration); err == nil || err.Error() != expected {
		t.Fatalf("Expected: %s. Failed with %s", expected, err)
	}
	configuration.Spec.RevisionTemplate.Spec.Container.Name = ""
	expected = fmt.Sprintf("The configuration spec must not set the field(s): %s", strings.Join(unwanted[1:], ", "))
	if err := ValidateConfiguration(testCtx)(nil, &configuration, &configuration); err == nil || err.Error() != expected {
		t.Fatalf("Expected: %s. Failed with %s", expected, err)
	}
	configuration.Spec.RevisionTemplate.Spec.Container.Resources = corev1.ResourceRequirements{}
	expected = fmt.Sprintf("The configuration spec must not set the field(s): %s", strings.Join(unwanted[2:], ", "))
	if err := ValidateConfiguration(testCtx)(nil, &configuration, &configuration); err == nil || err.Error() != expected {
		t.Fatalf("Expected: %s. Failed with %s", expected, err)
	}
}
