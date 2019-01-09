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

package reconciler

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

var (
	keyToFilter   = "testKey"
	valueToFilter = "testVal"
)

func config(annos map[string]string) *v1alpha1.Configuration {
	return &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "testSpace",
			Name:        "testName",
			Annotations: annos,
		},
	}
}

func TestAnnotationFilter(t *testing.T) {
	tests := []struct {
		name       string
		allowUnset bool
		in         interface{}
		want       bool
	}{{
		name: "non kubernetes object",
		in:   struct{}{},
		want: false,
	}, {
		name: "empty annotations",
		in:   config(nil),
		want: false,
	}, {
		name:       "empty annotations, allow unset",
		allowUnset: true,
		in:         config(nil),
		want:       true,
	}, {
		name: "other annotations",
		in:   config(map[string]string{"anotherKey": "anotherValue"}),
		want: false,
	}, {
		name:       "other annotations, allow unset",
		allowUnset: true,
		in:         config(map[string]string{"anotherKey": "anotherValue"}),
		want:       true,
	}, {
		name: "matching key, value mismatch",
		in:   config(map[string]string{keyToFilter: "testVal2"}),
		want: false,
	}, {
		name:       "matching key, value mismatch, allow unset",
		allowUnset: true,
		in:         config(map[string]string{keyToFilter: "testVal2"}),
		want:       false,
	}, {
		name: "match",
		in:   config(map[string]string{keyToFilter: valueToFilter}),
		want: true,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filter := AnnotationFilterFunc(keyToFilter, valueToFilter, test.allowUnset)
			got := filter(test.in)
			if got != test.want {
				t.Errorf("AnnotationFilterFunc did not return the expected result, got: %v", got)
			}
		})
	}
}
