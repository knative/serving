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

package resources

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func TestResourceBoundary(t *testing.T) {
	tests := []struct {
		name     string
		boundary resourceBoundary
		resource resource.Quantity
		want     resource.Quantity
	}{{
		name:     "resource within boundary",
		boundary: queueContainerRequestCPU,
		resource: resource.MustParse("55m"),
		want:     resource.MustParse("55m"),
	}, {
		name:     "resource lower than min boundary",
		boundary: queueContainerRequestCPU,
		resource: resource.MustParse("15m"),
		want:     resource.MustParse("25m"),
	},
		{
			name:     "resource lower than min boundary",
			boundary: queueContainerRequestCPU,
			resource: resource.MustParse("110m"),
			want:     resource.MustParse("100m"),
		}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.boundary.applyBoundary(test.resource)
			if test.want.Cmp(got) != 0 {
				t.Errorf("Expected quantity %v got %v ", test.want, got)
			}
		})
	}
}
