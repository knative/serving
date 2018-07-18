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

package test

import (
	"fmt"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

// states contains functions for asserting against the state of Knative Serving
// crds to see if they have achieved the states specified in the spec
// (https://github.com/knative/serving/blob/master/docs/spec/spec.md).

// AllRouteTrafficAtRevision will check the revision that route r is routing
// traffic to and return true if 100% of the traffic is routing to revisionName.
func AllRouteTrafficAtRevision(names ResourceNames) func(r *v1alpha1.Route) (bool, error) {
	return func(r *v1alpha1.Route) (bool, error) {
		for _, tt := range r.Status.Traffic {
			if tt.Percent == 100 {
				if tt.RevisionName != names.Revision {
					return true, fmt.Errorf("Expected traffic revision name to be %s but actually is %s", names.Revision, tt.RevisionName)
				}

				if tt.Name != names.TrafficTarget {
					return true, fmt.Errorf("Expected traffic target name to be %s but actually is %s", names.TrafficTarget, tt.Name)
				}

				return true, nil
			}
		}
		return false, nil
	}
}

// RouteTrafficToRevisionWithInClusterDNS will check the revision that route r is routing
// traffic using in cluster DNS and return true if the revision received the request.
func TODO_RouteTrafficToRevisionWithInClusterDNS(r *v1alpha1.Route) (bool, error) {
	if r.Status.DomainInternal == "" {
		return false, fmt.Errorf("Expected route %s to have in cluster dns status set", r.Name)
	}
	// TODO make a curl request from inside the cluster using
	// r.Status.DomainInternal to validate DNS is set correctly
	return true, nil
}

// ServiceTrafficToRevisionWithInClusterDNS will check the revision that route r is routing
// traffic using in cluster DNS and return true if the revision received the request.
func TODO_ServiceTrafficToRevisionWithInClusterDNS(s *v1alpha1.Service) (bool, error) {
	if s.Status.DomainInternal == "" {
		return false, fmt.Errorf("Expected service %s to have in cluster dns status set", s.Name)
	}
	// TODO make a curl request from inside the cluster using
	// s.Status.DomainInternal to validate DNS is set correctly
	return true, nil
}

// IsRevisionReady will check the status conditions of the revision and return true if the revision is
// ready to serve traffic. It will return false if the status indicates a state other than deploying
// or being ready. It will also return false if the type of the condition is unexpected.
func IsRevisionReady(r *v1alpha1.Revision) (bool, error) {
	return r.Status.IsReady(), nil
}

// IsServiceReady will check the status conditions of the service and return true if the service is
// ready. This means that its configurations and routes have all reported ready.
func IsServiceReady(s *v1alpha1.Service) (bool, error) {
	return s.Status.IsReady(), nil
}

// IsRouteReady will check the status conditions of the route and return true if the route is
// ready.
func IsRouteReady(r *v1alpha1.Route) (bool, error) {
	return r.Status.IsReady(), nil
}
