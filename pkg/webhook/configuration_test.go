/*
Copyright 2018 Google LLC. All Rights Reserved.
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
	"testing"

	"github.com/google/elafros/pkg/apis/ela/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidConfigurationAllowed(t *testing.T) {
	configuration := createConfiguration(testGeneration)

	if err := ValidateConfiguration(nil, &configuration, &configuration); err != nil {
		t.Fatalf("Expected allowed. Failed with %s", err)
	}
}

func TestEmptyConfigurationNotAllowed(t *testing.T) {
	if err := ValidateConfiguration(nil, nil, nil); err != errInvalidConfigurationInput {
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

	if err := ValidateConfiguration(nil, &configuration, &configuration); err != errEmptySpecInConfiguration {
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
			Generation: testGeneration,
			Template:   v1alpha1.Revision{},
		},
	}

	if err := ValidateConfiguration(nil, &configuration, &configuration); err != errEmptyTemplateInSpec {
		t.Fatalf("Expected: %s. Failed with %s", errEmptyTemplateInSpec, err)
	}
}

func TestNonEmptyStatusInConfiguration(t *testing.T) {
	configuration := createConfiguration(testGeneration)
	configuration.Status = v1alpha1.ConfigurationStatus{
		Latest: "latest version",
	}

	if err := ValidateConfiguration(nil, &configuration, &configuration); err != errNonEmptyStatusInConfiguration {
		t.Fatalf("Expected: %s. Failed with %s", errNonEmptyStatusInConfiguration, err)
	}
}
