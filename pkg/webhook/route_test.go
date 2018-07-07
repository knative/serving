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
	. "github.com/knative/serving/pkg/logging/testing"
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

	if err := ValidateRoute(TestContextWithLogger(t))(nil, &route, &route); err != nil {
		t.Fatalf("Expected allowed, but failed with: %s.", err)
	}
}

func TestEmptyRevisionAndConfigurationInOneTargetNotAllowed(t *testing.T) {
	route := createRouteWithTraffic([]v1alpha1.TrafficTarget{{
		Percent: 100,
	}})

	got := ValidateRoute(TestContextWithLogger(t))(nil, &route, &route)
	want := &v1alpha1.FieldError{
		Message: "Expected exactly one, got neither",
		Paths: []string{
			"spec.traffic[0].revisionName",
			"spec.traffic[0].configurationName",
		},
	}
	if got.Error() != want.Error() {
		t.Errorf("ValidateRoute() = %v, wanted %v", got, want)
	}
}

func TestBothRevisionAndConfigurationInOneTargetNotAllowed(t *testing.T) {
	route := createRouteWithTraffic([]v1alpha1.TrafficTarget{{
		RevisionName:      testRevisionName,
		ConfigurationName: testConfigurationName,
		Percent:           100,
	}})

	got := ValidateRoute(TestContextWithLogger(t))(nil, &route, &route)
	want := &v1alpha1.FieldError{
		Message: "Expected exactly one, got both",
		Paths: []string{
			"spec.traffic[0].revisionName",
			"spec.traffic[0].configurationName",
		},
	}
	if got.Error() != want.Error() {
		t.Errorf("ValidateRoute() = %v, wanted %v", got, want)
	}
}

func TestNegativeTargetPercentNotAllowed(t *testing.T) {
	route := createRouteWithTraffic([]v1alpha1.TrafficTarget{{
		RevisionName: testRevisionName,
		Percent:      -20,
	}})

	got := ValidateRoute(TestContextWithLogger(t))(nil, &route, &route)
	want := &v1alpha1.FieldError{
		Message: `invalid value "-20"`,
		Paths:   []string{"spec.traffic[0].percent"},
	}
	if got.Error() != want.Error() {
		t.Errorf("ValidateRoute() = %v, wanted %v", got, want)
	}
}

func TestNotAllowedIfTrafficPercentSumIsNot100(t *testing.T) {
	route := createRouteWithTraffic([]v1alpha1.TrafficTarget{{
		ConfigurationName: "test-configuration-1",
	}, {
		ConfigurationName: "test-configuration-2",
		Percent:           50,
	}})

	got := ValidateRoute(TestContextWithLogger(t))(nil, &route, &route)
	want := &v1alpha1.FieldError{
		Message: "Traffic targets sum to 50, want 100",
		Paths:   []string{"spec.traffic"},
	}
	if got.Error() != want.Error() {
		t.Errorf("ValidateRoute() = %v, wanted %v", got, want)
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

	got := ValidateRoute(TestContextWithLogger(t))(nil, &route, &route)
	want := &v1alpha1.FieldError{
		Message: `Multiple definitions for "test"`,
		Paths:   []string{"spec.traffic[0].name", "spec.traffic[1].name"},
	}
	if got.Error() != want.Error() {
		t.Errorf("ValidateRoute() = %v, wanted %v", got, want)
	}
}
