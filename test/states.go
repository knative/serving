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
	corev1 "k8s.io/api/core/v1"
)

// states contains functions for asserting against the state of Knative Serving
// crds to see if they have achieved the states specified in the spec
// (https://github.com/knative/serving/blob/master/docs/spec/spec.md).

// AllRouteTrafficAtRevision will check the revision that routeName is routing
// traffic to and return true if 100% of the traffic is routing to revisionName.
func AllRouteTrafficAtRevision(routeName string, revisionName string) func(r *v1alpha1.Route) (bool, error) {
	return func(r *v1alpha1.Route) (bool, error) {
		for _, tt := range r.Status.Traffic {
			if tt.RevisionName == revisionName {
				if r.Status.Traffic[0].Percent == 100 {
					if r.Status.Traffic[0].Name != routeName {
						return true, fmt.Errorf("Expected traffic name to be %s but actually is %s", revisionName, r.Status.Traffic[0].Name)
					}
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
		if cond := r.Status.GetCondition(v1alpha1.RevisionConditionReady); cond == nil {
			return false, nil
		} else {
			return cond.Status == corev1.ConditionTrue, nil
		}
	}
}
