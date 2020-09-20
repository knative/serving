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

package v1beta1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GetGroupVersionKind returns the GroupVersionKind.
func (r *Route) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("Route")
}

// IsReady returns if the revision is ready to serve the requested revision
// and the revision resource has been observed.
func (r *Route) IsReady() bool {
	rs := r.Status
	return rs.ObservedGeneration == r.Generation &&
		rs.GetCondition(RouteConditionReady).IsTrue()
}

// IsFailed returns true if the resource has observed the latest generation and ready is false.
func (r *Route) IsFailed() bool {
	rs := r.Status
	return rs.ObservedGeneration == r.Generation &&
		rs.GetCondition(RouteConditionReady).IsFalse()
}
