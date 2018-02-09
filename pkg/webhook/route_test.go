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

func createRouteWithTraffic(trafficTargets []v1alpha1.TrafficTarget) v1alpha1.Route {
	return v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      esName,
		},
		Spec: v1alpha1.RouteSpec{
			Generation:   testGeneration,
			DomainSuffix: testDomain,
			Traffic:      trafficTargets,
		},
	}
}

func TestValidRouteWithTrafficAllowed(t *testing.T) {
	route := createRouteWithTraffic(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				Configuration: "test-revision-template-1",
				Percent:          50,
			},
			v1alpha1.TrafficTarget{
				Configuration: "test-revision-template-2",
				Percent:          50,
			},
		})

	err := ValidateRoute(nil, &route, &route)

	if err != nil {
		t.Fatalf("Expected allowed, but failed with: %s.", err)
	}
}

func TestEmptyTrafficTargetWithoutTrafficAllowed(t *testing.T) {
	route := createRouteWithTraffic(nil)

	err := ValidateRoute(nil, &route, &route)

	if err != nil {
		t.Fatalf("Expected allowed, but failed with: %s.", err)
	}
}

func TestNoneRouteTypeForOldResourceNotAllowed(t *testing.T) {
	revision := v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRevisionName,
		},
	}

	err := ValidateRoute(nil, &revision, &revision)

	if err == nil || err.Error() != "Failed to convert old into Route" {
		t.Fatalf(
			"Expected: Failed to convert old into Route. Failed with: %s.", err)
	}
}

func TestNoneRouteTypeForNewResourceNotAllowed(t *testing.T) {
	revision := v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRevisionName,
		},
	}

	err := ValidateRoute(nil, nil, &revision)

	if err == nil || err.Error() != "Failed to convert new into Route" {
		t.Fatalf(
			"Expected: Failed to convert new into Route. Failed with: %s.", err)
	}
}

func TestEmptyRevisionAndConfigurationInOneTargetNotAllowed(t *testing.T) {
	route := createRouteWithTraffic(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				Percent: 100,
			},
		})

	err := ValidateRoute(nil, &route, &route)

	if err == nil || err.Error() != errInvalidRevisionsMessage {
		t.Fatalf(
			"Expected: %s. Failed with: %s.", errInvalidRevisionsMessage, err)
	}
}

func TestBothRevisionAndConfigurationInOneTargetNotAllowed(t *testing.T) {
	route := createRouteWithTraffic(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				Revision:         testRevisionName,
				Configuration: "test-revision-template",
				Percent:          100,
			},
		})

	err := ValidateRoute(nil, &route, &route)

	if err == nil || err.Error() != errInvalidRevisionsMessage {
		t.Fatalf(
			"Expected: %s. Failed with: %s.", errInvalidRevisionsMessage, err)
	}
}

func TestNegativeTargetPercentNotAllowed(t *testing.T) {
	route := createRouteWithTraffic(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				Revision: testRevisionName,
				Percent:  -20,
			},
		})

	err := ValidateRoute(nil, &route, &route)

	if err == nil || err.Error() != errNegativeTargetPercentMessage {
		t.Fatalf(
			"Expected: %s. Failed with: %s.", errNegativeTargetPercentMessage, err)
	}
}

func TestNotAllowedIfTrafficPercentSumIsNot100(t *testing.T) {
	route := createRouteWithTraffic(
		[]v1alpha1.TrafficTarget{
			v1alpha1.TrafficTarget{
				Configuration: "test-revision-template-1",
			},
			v1alpha1.TrafficTarget{
				Configuration: "test-revision-template-2",
				Percent:          50,
			},
		})

	err := ValidateRoute(nil, &route, &route)

	if err == nil || err.Error() != errInvalidTargetPercentSumMessage {
		t.Fatalf(
			"Expected: %s. Failed with: %s.", errInvalidTargetPercentSumMessage, err)
	}
}
