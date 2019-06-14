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

package v1alpha1

// crd contains functions that construct boilerplate CRD definitions.

import (
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

const (
	// Default for user containers in e2e tests. This value is lower than the general
	// Knative's default so as to run more effectively in CI with limited resources.
	defaultRequestCPU = "100m"
)

// ResourceObjects holds types of the resource objects.
type ResourceObjects struct {
	Route    *v1alpha1.Route
	Config   *v1alpha1.Configuration
	Service  *v1alpha1.Service
	Revision *v1alpha1.Revision
}

// LogResourceObject logs the resource object with the resource name and value
func LogResourceObject(t *testing.T, value ResourceObjects) {
	t.Logf("resource %s", spew.Sprint(value))
}
