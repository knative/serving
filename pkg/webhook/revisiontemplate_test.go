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

func TestValidRevisionTemplateAllowed(t *testing.T) {
	revisionTemplate := createRevisionTemplate(testGeneration)

	err := ValidateRevisionTemplate(nil, &revisionTemplate, &revisionTemplate)

	if err != nil {
		t.Fatalf("Valid revision template should pass, but failed with:  %s.", err)
	}
}

func TestEmptyRevisionTemplateNotAllowed(t *testing.T) {
	err := ValidateRevisionTemplate(nil, nil, nil)
	if err == nil || err.Error() != "Failed to convert new into RevisionTemplate" {
		t.Fatalf("Expected: Failed to convert new into RevisionTemplate. Failed with %s", err)
	}
}

func TestEmptySpecInRevisionTemplateNotAllowed(t *testing.T) {
	revisionTemplate := v1alpha1.RevisionTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      rtName,
		},
		Spec: v1alpha1.RevisionTemplateSpec{},
	}

	err := ValidateRevisionTemplate(nil, &revisionTemplate, &revisionTemplate)

	if err == nil || err.Error() != emptySpecInRevisionTemplateErrorMessage {
		t.Fatalf("Expected: %s. Failed with %s", emptySpecInRevisionTemplateErrorMessage, err)
	}
}

func TestEmptyTemplateInSpecNotAllowed(t *testing.T) {
	revisionTemplate := v1alpha1.RevisionTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      rtName,
		},
		Spec: v1alpha1.RevisionTemplateSpec{
			Generation: testGeneration,
			Template:   v1alpha1.Revision{},
		},
	}

	err := ValidateRevisionTemplate(nil, &revisionTemplate, &revisionTemplate)

	if err == nil || err.Error() != emptyTemplateInSpecErrorMessage {
		t.Fatalf("Expected: %s. Failed with %s", emptyTemplateInSpecErrorMessage, err)
	}
}
