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

func createElaServiceWithTraffic(trafficTargets []v1alpha1.TrafficTarget) v1alpha1.ElaService {
	return v1alpha1.ElaService{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      esName,
		},
		Spec: v1alpha1.ElaServiceSpec{
			Generation:   testGeneration,
			DomainSuffix: testDomain,
			Traffic:      trafficTargets,
		},
	}
}

func TestValidElaServiceAllowed(t *testing.T) {
	elaService := createElaServiceWithTraffic(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				RevisionTemplate: "test-revision-template-1",
				Percent:          50,
			},
			v1alpha1.TrafficTarget{
				RevisionTemplate: "test-revision-template-2",
				Percent:          50,
			},
		})

	err := ValidateElaService(nil, &elaService, &elaService)

	if err != nil {
		t.Fatalf("Expected allowed, but failed with: %s.", err)
	}
}

func TestNoneElaServiceTypeForOldResourceNotAllowed(t *testing.T) {
	revision := v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRevisionName,
		},
	}

	err := ValidateElaService(nil, &revision, &revision)

	if err == nil || err.Error() != "Failed to convert old into ElaService." {
		t.Fatalf(
			"Expected: Failed to convert old into ElaService. Failed with: %s.", err)
	}
}

func TestNoneElaServiceTypeForNewResourceNotAllowed(t *testing.T) {
	revision := v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRevisionName,
		},
	}

	err := ValidateElaService(nil, nil, &revision)

	if err == nil || err.Error() != "Failed to convert new into ElaService." {
		t.Fatalf(
			"Expected: Failed to convert new into ElaService. Failed with: %s.", err)
	}
}

func TestEmptyTrafficTargetNotAllowed(t *testing.T) {
	elaService := createElaServiceWithTraffic(nil)
	err := ValidateElaService(nil, &elaService, &elaService)

	if err == nil || err.Error() != noTrafficErrorMessage {
		t.Fatalf(
			"Expected: %s. Failed with: %s.", noTrafficErrorMessage, err)
	}
}

func TestEmptyRevisionAndRevisionTemplateInOneTargetNotAllowed(t *testing.T) {
	elaService := createElaServiceWithTraffic(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				Percent: 100,
			},
		})

	err := ValidateElaService(nil, &elaService, &elaService)

	if err == nil || err.Error() != noRevisionsErrorMessage {
		t.Fatalf(
			"Expected: %s. Failed with: %s.", noRevisionsErrorMessage, err)
	}
}

func TestBothRevisionAndRevisionTemplateInOneTargetNotAllowed(t *testing.T) {
	elaService := createElaServiceWithTraffic(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				Revision:         testRevisionName,
				RevisionTemplate: "test-revision-template",
				Percent:          100,
			},
		})

	err := ValidateElaService(nil, &elaService, &elaService)

	if err == nil || err.Error() != conflictRevisionsErrorMessage {
		t.Fatalf(
			"Expected: %s. Failed with: %s.", conflictRevisionsErrorMessage, err)
	}
}

func TestNegativeTargetPercentNotAllowed(t *testing.T) {
	elaService := createElaServiceWithTraffic(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				Revision: testRevisionName,
				Percent:  -20,
			},
		})

	err := ValidateElaService(nil, &elaService, &elaService)

	if err == nil || err.Error() != negativeTargetPercentErrorMessage {
		t.Fatalf(
			"Expected: %s. Failed with: %s.", negativeTargetPercentErrorMessage, err)
	}
}

func TestNotAllowedIfTrafficPercentSumIsNot100(t *testing.T) {
	elaService := createElaServiceWithTraffic(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				RevisionTemplate: "test-revision-template-1",
			},
			v1alpha1.TrafficTarget{
				RevisionTemplate: "test-revision-template-2",
				Percent:          50,
			},
		})

	err := ValidateElaService(nil, &elaService, &elaService)

	if err == nil || err.Error() != targetPercentSumErrorMessage {
		t.Fatalf(
			"Expected: %s. Failed with: %s.", targetPercentSumErrorMessage, err)
	}
}
