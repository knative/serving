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

func config(annos, labels map[string]string) *v1alpha1.Configuration {
	return &v1alpha1.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "testSpace",
			Name:        "testName",
			Annotations: annos,
			Labels:      labels,
		},
	}
}

type params struct {
	name       string
	allowUnset bool
	in         interface{}
	want       bool
}

func TestAnnotationFilter(t *testing.T) {
	tests := []params{{
		name: "non kubernetes object",
		in:   struct{}{},
		want: false,
	}, {
		name: "empty annotations",
		in:   config(nil, nil),
		want: false,
	}, {
		name:       "empty annotations, allow unset",
		allowUnset: true,
		in:         config(nil, nil),
		want:       true,
	}, {
		name: "other annotations",
		in:   config(map[string]string{"anotherKey": "anotherValue"}, nil),
		want: false,
	}, {
		name:       "other annotations, allow unset",
		allowUnset: true,
		in:         config(map[string]string{"anotherKey": "anotherValue"}, nil),
		want:       true,
	}, {
		name: "matching key, value mismatch",
		in:   config(map[string]string{keyToFilter: "testVal2"}, nil),
		want: false,
	}, {
		name:       "matching key, value mismatch, allow unset",
		allowUnset: true,
		in:         config(map[string]string{keyToFilter: "testVal2"}, nil),
		want:       false,
	}, {
		name: "match",
		in:   config(map[string]string{keyToFilter: valueToFilter}, nil),
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

func TestLabelExistsFilterFunc(t *testing.T) {
	ti := []params{{
		name: "label exists",
		in:   config(nil, map[string]string{keyToFilter: valueToFilter}),
		want: true,
	}, {
		name: "empty labels",
		in:   config(nil, map[string]string{}),
		want: false,
	}, {
		name: "non-empty map, the required label doesn't exist",
		in:   config(nil, map[string]string{"randomLabel": ""}),
		want: false,
	}, {
		name: "non kubernetes object",
		in:   struct{}{},
		want: false,
	}}

	for _, test := range ti {
		t.Run(test.name, func(t *testing.T) {
			filter := LabelExistsFilterFunc(keyToFilter)
			got := filter(test.in)
			if got != test.want {
				t.Errorf("LabelExistsFilterFunc did not return the expected result, got: %v", got)
			}
		})
	}
}

func TestLabelFilterFunc(t *testing.T) {
	ti := []params{{
		name:       "label matches no unset",
		in:         config(nil, map[string]string{keyToFilter: valueToFilter}),
		allowUnset: false,
		want:       true,
	}, {
		name:       "label matches with unset",
		in:         config(nil, map[string]string{keyToFilter: valueToFilter}),
		allowUnset: true,
		want:       true,
	}, {
		name:       "label mismatch no unset",
		in:         config(nil, map[string]string{keyToFilter: "otherval"}),
		allowUnset: false,
		want:       false,
	}, {
		name:       "label mismatch with unset",
		in:         config(nil, map[string]string{keyToFilter: "otherval"}),
		allowUnset: true,
		want:       false,
	}, {
		name:       "label missing no unset",
		in:         config(nil, map[string]string{}),
		allowUnset: false,
		want:       false,
	}, {
		name:       "label missing with unset",
		in:         config(nil, map[string]string{}),
		allowUnset: true,
		want:       true,
	}, {
		name:       "nil labels no unset",
		in:         config(nil, nil),
		allowUnset: false,
		want:       false,
	}, {
		name:       "nil labels with unset",
		in:         config(nil, nil),
		allowUnset: true,
		want:       true,
	}}

	for _, test := range ti {
		t.Run(test.name, func(t *testing.T) {
			filter := LabelFilterFunc(keyToFilter, valueToFilter, test.allowUnset)
			got := filter(test.in)
			if got != test.want {
				t.Errorf("LabelFilterFunc did not return expected result. Got: %v", got)
			}
		})
	}
}

func TestChainFilterFuncs(t *testing.T) {
	tc := []struct {
		name  string
		chain []bool
		want  bool
	}{{
		name:  "single true",
		chain: []bool{true},
		want:  true,
	}, {
		name:  "single false",
		chain: []bool{false},
		want:  false,
	}, {
		name:  "second false",
		chain: []bool{true, false},
		want:  false,
	}, {
		name:  "multi true",
		chain: []bool{true, true},
		want:  true,
	}}

	for _, test := range tc {
		t.Run(test.name, func(t *testing.T) {
			filters := make([]func(interface{}) bool, len(test.chain))
			for i, chainVal := range test.chain {
				filters[i] = func(interface{}) bool {
					return chainVal
				}
			}
			filter := ChainFilterFuncs(filters...)
			got := filter(nil)
			if got != test.want {
				t.Errorf("ChainFilterFuncs did not return expected result. Got: %v", got)
			}
		})
	}
}
