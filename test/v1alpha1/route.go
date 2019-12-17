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

// route.go provides methods to perform actions on the route resource.

package v1alpha1

import (
	"context"
	"fmt"

	"github.com/davecgh/go-spew/spew"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/pkg/ptr"
	pkgTest "knative.dev/pkg/test"
	"knative.dev/pkg/test/logging"
	"knative.dev/pkg/test/spoof"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"

	v1alpha1testing "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
)

// CreateRoute creates a route in the given namespace using the route name in names
func CreateRoute(t pkgTest.T, clients *test.Clients, names test.ResourceNames, fopt ...v1alpha1testing.RouteOption) (*v1alpha1.Route, error) {
	fopt = append(fopt, v1alpha1testing.WithSpecTraffic(v1alpha1.TrafficTarget{
		TrafficTarget: v1.TrafficTarget{
			Tag:               names.TrafficTarget,
			ConfigurationName: names.Config,
			Percent:           ptr.Int64(100),
		},
	}))
	route := v1alpha1testing.Route(test.ServingNamespace, names.Route, fopt...)
	LogResourceObject(t, ResourceObjects{Route: route})
	return clients.ServingAlphaClient.Routes.Create(route)
}

// RetryingRouteInconsistency retries common requests seen when creating a new route
// TODO(5573): Remove this.
func RetryingRouteInconsistency(innerCheck spoof.ResponseChecker) spoof.ResponseChecker {
	return func(resp *spoof.Response) (bool, error) {
		// If we didn't match any retryable codes, invoke the ResponseChecker that we wrapped.
		return innerCheck(resp)
	}
}

// WaitForRouteState polls the status of the Route called name from client every
// PollInterval until inState returns `true` indicating it is done, returns an
// error or PollTimeout. desc will be used to name the metric that is emitted to
// track how long it took for name to get into the state checked by inState.
func WaitForRouteState(client *test.ServingAlphaClients, name string, inState func(r *v1alpha1.Route) (bool, error), desc string) error {
	span := logging.GetEmitableSpan(context.Background(), fmt.Sprintf("WaitForRouteState/%s/%s", name, desc))
	defer span.End()

	var lastState *v1alpha1.Route
	waitErr := wait.PollImmediate(test.PollInterval, test.PollTimeout, func() (bool, error) {
		var err error
		lastState, err = client.Routes.Get(name, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		return inState(lastState)
	})

	if waitErr != nil {
		return fmt.Errorf("route %q is not in desired state, got: %+v: %w", name, lastState, waitErr)
	}
	return nil
}

// CheckRouteState verifies the status of the Route called name from client
// is in a particular state by calling `inState` and expecting `true`.
// This is the non-polling variety of WaitForRouteState
func CheckRouteState(client *test.ServingAlphaClients, name string, inState func(r *v1alpha1.Route) (bool, error)) error {
	r, err := client.Routes.Get(name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if done, err := inState(r); err != nil {
		return err
	} else if !done {
		return fmt.Errorf("route %q is not in desired state, got: %+v", name, r)
	}
	return nil
}

// IsRouteReady will check the status conditions of the route and return true if the route is
// ready.
func IsRouteReady(r *v1alpha1.Route) (bool, error) {
	return r.Generation == r.Status.ObservedGeneration && r.Status.IsReady(), nil
}

// IsRouteNotReady will check the status conditions of the route and return true if the route is
// not ready.
func IsRouteNotReady(r *v1alpha1.Route) (bool, error) {
	return !r.Status.IsReady(), nil
}

// AllRouteTrafficAtRevision will check the revision that route r is routing
// traffic to and return true if 100% of the traffic is routing to revisionName.
func AllRouteTrafficAtRevision(names test.ResourceNames) func(r *v1alpha1.Route) (bool, error) {
	return func(r *v1alpha1.Route) (bool, error) {
		for _, tt := range r.Status.Traffic {
			if tt.Percent != nil && *tt.Percent == 100 {
				if tt.RevisionName != names.Revision {
					return true, fmt.Errorf("expected traffic revision name to be %s but actually is %s: %s", names.Revision, tt.RevisionName, spew.Sprint(r))
				}

				if tt.Tag != names.TrafficTarget {
					return true, fmt.Errorf("expected traffic target name to be %s but actually is %s: %s", names.TrafficTarget, tt.Tag, spew.Sprint(r))
				}

				return true, nil
			}
		}
		return false, nil
	}
}
