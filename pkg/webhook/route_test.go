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
	"testing"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func createRouteWithTraffic(trafficTargets []v1alpha1.TrafficTarget) v1alpha1.Route {
	return v1alpha1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRouteName,
		},
		Spec: v1alpha1.RouteSpec{
			Generation: testGeneration,
			Traffic:    trafficTargets,
		},
	}
}

func TestValidRouteWithTrafficAllowed(t *testing.T) {
	route := createRouteWithTraffic([]v1alpha1.TrafficTarget{{
		ConfigurationName: "test-configuration-1",
		Percent:           50,
	}, {
		ConfigurationName: "test-configuration-2",
		Percent:           50,
	}})

	if err := ValidateRoute(testCtx)(nil, &route, &route); err != nil {
		t.Fatalf("Expected allowed, but failed with: %s.", err)
	}
}

func TestEmptyTrafficTargetWithoutTrafficAllowed(t *testing.T) {
	route := createRouteWithTraffic(nil)

	if err := ValidateRoute(testCtx)(nil, &route, &route); err != nil {
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

	if err := ValidateRoute(testCtx)(nil, &revision, &revision); err != errInvalidRouteInput {
		t.Fatalf("Expected: %s. Failed with: %s.", errInvalidRouteInput, err)
	}
}

func TestNoneRouteTypeForNewResourceNotAllowed(t *testing.T) {
	revision := v1alpha1.Revision{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      testRevisionName,
		},
	}

	if err := ValidateRoute(testCtx)(nil, nil, &revision); err != errInvalidRouteInput {
		t.Fatalf("Expected: %s. Failed with: %s.", errInvalidRouteInput, err)
	}
}

func TestEmptyRevisionAndConfigurationInOneTargetNotAllowed(t *testing.T) {
	route := createRouteWithTraffic([]v1alpha1.TrafficTarget{{
		Percent: 100,
	}})

	if err := ValidateRoute(testCtx)(nil, &route, &route); err != errInvalidRevisions {
		t.Fatalf("Expected: %s. Failed with: %s.", errInvalidRevisions, err)
	}
}

func TestBothRevisionAndConfigurationInOneTargetNotAllowed(t *testing.T) {
	route := createRouteWithTraffic([]v1alpha1.TrafficTarget{{
		RevisionName:      testRevisionName,
		ConfigurationName: testConfigurationName,
		Percent:           100,
	}})

	if err := ValidateRoute(testCtx)(nil, &route, &route); err != errInvalidRevisions {
		t.Fatalf("Expected: %s. Failed with: %s.", errInvalidRevisions, err)
	}
}

func TestNegativeTargetPercentNotAllowed(t *testing.T) {
	route := createRouteWithTraffic([]v1alpha1.TrafficTarget{{
		RevisionName: testRevisionName,
		Percent:      -20,
	}})

	if err := ValidateRoute(testCtx)(nil, &route, &route); err != errNegativeTargetPercent {
		t.Fatalf("Expected: %s. Failed with: %s.", errNegativeTargetPercent, err)
	}
}

func TestNotAllowedIfTrafficPercentSumIsNot100(t *testing.T) {
	route := createRouteWithTraffic([]v1alpha1.TrafficTarget{{
		ConfigurationName: "test-configuration-1",
	}, {
		ConfigurationName: "test-configuration-2",
		Percent:           50,
	}})

	if err := ValidateRoute(testCtx)(nil, &route, &route); err != errInvalidTargetPercentSum {
		t.Fatalf("Expected: %s. Failed with: %s.", errInvalidTargetPercentSum, err)
	}
}

func TestNotAllowedIfTrafficNamesNotUnique(t *testing.T) {
	route := createRouteWithTraffic([]v1alpha1.TrafficTarget{{
		Name:              "test",
		ConfigurationName: "test-configuration-1",
		Percent:           50,
	}, {
		Name:              "test",
		ConfigurationName: "test-configuration-2",
		Percent:           50,
	}})

	if err := ValidateRoute(testCtx)(nil, &route, &route); err != errTrafficTargetsNotUnique {
		t.Fatalf("Expected: %s. Failed with: %s.", errTrafficTargetsNotUnique, err)
	}
}
