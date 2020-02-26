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

package v1

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/pkg/ptr"
	"knative.dev/pkg/test/logging"
	"knative.dev/pkg/test/spoof"
	v1 "knative.dev/serving/pkg/apis/serving/v1"

	pkgTest "knative.dev/pkg/test"
	rtesting "knative.dev/serving/pkg/testing/v1"
	"knative.dev/serving/test"
)

// Route returns a Route object in namespace using the route and configuration
// names in names.
func Route(names test.ResourceNames, fopt ...rtesting.RouteOption) *v1.Route {
	route := &v1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.Route,
		},
		Spec: v1.RouteSpec{
			Traffic: []v1.TrafficTarget{{
				Tag:               names.TrafficTarget,
				ConfigurationName: names.Config,
				Percent:           ptr.Int64(100),
			}},
		},
	}

	for _, opt := range fopt {
		opt(route)
	}

	return route
}

// CreateRoute creates a route in the given namespace using the route name in names
func CreateRoute(t pkgTest.T, clients *test.Clients, names test.ResourceNames, fopt ...rtesting.RouteOption) (*v1.Route, error) {
	route := Route(names, fopt...)
	LogResourceObject(t, ResourceObjects{Route: route})
	return clients.ServingClient.Routes.Create(route)
}

// WaitForRouteState polls the status of the Route called name from client every
// PollInterval until inState returns `true` indicating it is done, returns an
// error or timeout. desc will be used to name the metric that is emitted to
// track how long it took for name to get into the state checked by inState.
func WaitForRouteState(client *test.ServingClients, name string, inState func(r *v1.Route) (bool, error), desc string) error {
	span := logging.GetEmitableSpan(context.Background(), fmt.Sprintf("WaitForRouteState/%s/%s", name, desc))
	defer span.End()

	var lastState *v1.Route
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
func CheckRouteState(client *test.ServingClients, name string, inState func(r *v1.Route) (bool, error)) error {
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
func IsRouteReady(r *v1.Route) (bool, error) {
	return r.Generation == r.Status.ObservedGeneration && r.Status.IsReady(), nil
}

// IsRouteNotReady will check the status conditions of the route and return true if the route is
// not ready.
func IsRouteNotReady(r *v1.Route) (bool, error) {
	return !r.Status.IsReady(), nil
}

// RetryingRouteInconsistency retries common requests seen when creating a new route
func RetryingRouteInconsistency(innerCheck spoof.ResponseChecker) spoof.ResponseChecker {
	return func(resp *spoof.Response) (bool, error) {
		// If we didn't match any retryable codes, invoke the ResponseChecker that we wrapped.
		return innerCheck(resp)
	}
}

// AllRouteTrafficAtRevision will check the revision that route r is routing
// traffic to and return true if 100% of the traffic is routing to revisionName.
func AllRouteTrafficAtRevision(names test.ResourceNames) func(r *v1.Route) (bool, error) {
	return func(r *v1.Route) (bool, error) {
		for _, tt := range r.Status.Traffic {
			if tt.Percent != nil && *tt.Percent == 100 && tt.RevisionName == names.Revision && tt.Tag == names.TrafficTarget {
				return true, nil
			}
		}
		return false, nil
	}
}
