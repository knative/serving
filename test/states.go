/*
Copyright 2018 Google Inc. All Rights Reserved.
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

	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// states contains functions for asserting against the state of Elafros
// crds to see if they have achieved the states specified in the spec
// (https://github.com/elafros/elafros/blob/master/docs/spec/spec.md).

// AllRouteTrafficeAtRevision will check the revision that routeName is routing
// traffic to and return true if 100% of the traffic is routing to revisionName.
func AllRouteTrafficAtRevision(routeName string, revisionName string) func(r *v1alpha1.Route) (bool, error) {
	return func(r *v1alpha1.Route) (bool, error) {
		if len(r.Status.Traffic) > 0 {
			if len(r.Status.Traffic) != 1 {
				return true, fmt.Errorf("Expected Route to have only one configurated Traffic but had %d. Route: %v", len(r.Status.Traffic), r)
			}
			if r.Status.Traffic[0].RevisionName == revisionName {
				if r.Status.Traffic[0].Percent != 100 {
					return true, fmt.Errorf("Expected 100%% of traffic to go to Revision %s but actually is %d", revisionName, r.Status.Traffic[0].Percent)
				}
				if r.Status.Traffic[0].Name != routeName {
					return true, fmt.Errorf("Expected traffic name to be %s but actually is %s", revisionName, r.Status.Traffic[0].Name)
				}
				return true, nil
			}
		}
		return false, nil
	}
}

// IsRevisionReady will check the status conditions of revision revisionName and return true if the revision is
// ready to serve traffic. It will return false if the status indicates a state other than deploying
// or being ready. It will also return false if the type of the condition is unexpected.
func IsRevisionReady(revisionName string) func(r *v1alpha1.Revision) (bool, error) {
	return func(r *v1alpha1.Revision) (bool, error) {
		if len(r.Status.Conditions) > 0 {
			if r.Status.Conditions[0].Type != v1alpha1.RevisionConditionType("Ready") {
				return true, fmt.Errorf("Expected Revision to have a \"Ready\" status but only had %s", r.Status.Conditions[0].Type)
			}
			if r.Status.Conditions[0].Status == corev1.ConditionStatus("Unknown") {
				if r.Status.Conditions[0].Reason != "Deploying" {
					return true, fmt.Errorf("If the Revision isn't ready the reason should be to be \"Deploying\" but was %s", r.Status.Conditions[0].Reason)
				}
			} else {
				if r.Status.Conditions[0].Status != corev1.ConditionStatus("True") {
					return true, fmt.Errorf("Expected Revision Status Condition Status to be True or Unknown but was %s", r.Status.Conditions[0].Status)
				}
				if r.Status.Conditions[0].Reason != "ServiceReady" {
					return true, fmt.Errorf("If the Revision is ready the Reason should be \"ServiceReady\" but was %s", r.Status.Conditions[0].Status)
				}
				return true, nil
			}
		}
		return false, nil
	}
}
