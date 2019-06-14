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
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/knative/pkg/apis/duck"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
)

const (
	// Default for user containers in e2e tests. This value is lower than the general
	// Knative's default so as to run more effectively in CI with limited resources.
	defaultRequestCPU = "100m"
	interval          = 1 * time.Second
	timeout           = 10 * time.Minute
)

// TODO(dangerd): Move function to duck.CreateBytePatch
func createPatch(cur, desired interface{}) ([]byte, error) {
	patch, err := duck.CreatePatch(cur, desired)
	if err != nil {
		return nil, err
	}
	return patch.MarshalJSON()
}

// ResourceObjects holds types of the resource objects.
type ResourceObjects struct {
	Route    *v1beta1.Route
	Config   *v1beta1.Configuration
	Service  *v1beta1.Service
	Revision *v1beta1.Revision
}

// LogResourceObject logs the resource object with the resource name and value
func LogResourceObject(t *testing.T, value ResourceObjects) {
	t.Logf("resource %s", spew.Sprint(value))
}
