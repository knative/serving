/*
Copyright 2020 The Knative Authors
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

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	duckv1 "knative.dev/pkg/apis/duck/v1"
)

func TestServiceGetStatus(t *testing.T) {
	status := &duckv1.Status{}
	service := Service{
		Status: ServiceStatus{
			Status: *status,
		},
	}

	if !cmp.Equal(service.GetStatus(), status) {
		t.Errorf("service.GetStatus() did not return expected status. Actual:%v Expected:%v",
			service.GetStatus(), status)
	}
}
