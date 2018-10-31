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

package autoscaling

import (
	"reflect"
	"testing"
)

func TestInherit(t *testing.T) {
	cases := []struct {
		name   string
		parent map[string]string
		child  map[string]string
		want   map[string]string
	}{{
		name:   "inherit autoscaling annotations",
		parent: map[string]string{"autoscaling.knative.dev/class": "kpa"},
		child:  map[string]string{},
		want:   map[string]string{"autoscaling.knative.dev/class": "kpa"},
	}, {
		name:   "inherit autoscaling internal annotations",
		parent: map[string]string{"autoscaling.internal.knative.dev/class": "kpa"},
		child:  map[string]string{},
		want:   map[string]string{"autoscaling.internal.knative.dev/class": "kpa"},
	}, {
		name:   "inherit skips non-autoscaling annotations",
		parent: map[string]string{"other.knative.dev/class": "thing"},
		child:  map[string]string{},
		want:   map[string]string{},
	}, {
		name:   "inherit to nil map",
		parent: map[string]string{"autoscaling.knative.dev/class": "kpa"},
		child:  nil,
		want:   map[string]string{"autoscaling.knative.dev/class": "kpa"},
	}, {
		name:   "inherit from nil map",
		parent: nil,
		child:  map[string]string{},
		want:   map[string]string{},
	}, {
		name: "non-inherited autoscaling annotations unmodified",
		parent: map[string]string{
			"autoscaling.knative.dev/class": "kpa",
		},
		child: map[string]string{
			"autoscaling.knative.dev/class":       "hpa",
			"autoscaling.knative.dev/minReplicas": "1",
		},
		want: map[string]string{
			"autoscaling.knative.dev/class":       "kpa",
			"autoscaling.knative.dev/minReplicas": "1",
		},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Inherit(tc.child, tc.parent); !reflect.DeepEqual(tc.want, got) {
				t.Errorf("%q expected %v. got %v", tc.name, tc.want, got)
			}
		})
	}
}

func TestIsInheritedEqual(t *testing.T) {
	cases := []struct {
		name   string
		parent map[string]string
		child  map[string]string
		want   bool
	}{{
		name:   "empty equal",
		parent: map[string]string{},
		child:  map[string]string{},
		want:   true,
	}, {
		name:   "inherited autoscaling annotations equal",
		parent: map[string]string{"autoscaling.knative.dev/class": "kpa"},
		child:  map[string]string{"autoscaling.knative.dev/class": "kpa"},
		want:   true,
	}, {
		name:   "inherited autoscaling annotations not equal",
		parent: map[string]string{"autoscaling.knative.dev/class": "kpa"},
		child:  map[string]string{"autoscaling.knative.dev/class": "hpa"},
		want:   false,
	}, {
		name: "inherted autoscaling annotations equal but not alone in parent",
		parent: map[string]string{
			"autoscaling.knative.dev/class": "kpa",
			"other.knative.dev/class":       "thing",
		},
		child: map[string]string{
			"autoscaling.knative.dev/class": "kpa",
		},
		want: true,
	}, {
		name: "inherted autoscaling annotations equal but not alone in child",
		parent: map[string]string{
			"autoscaling.knative.dev/class": "kpa",
		},
		child: map[string]string{
			"autoscaling.knative.dev/class": "kpa",
			"other.knative.dev/class":       "thing",
		},
		want: true,
	}, {
		name: "inherted autoscaling annotations equal and not alone in child and parent",
		parent: map[string]string{
			"autoscaling.knative.dev/class": "kpa",
			"other.knative.dev/class":       "thing1",
		},
		child: map[string]string{
			"autoscaling.knative.dev/class": "kpa",
			"other.knative.dev/class":       "thing2",
		},
		want: true,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsInheritedEqual(tc.child, tc.parent); tc.want != got {
				t.Errorf("%q expected %v. got %v", tc.name, tc.want, got)
			}
		})
	}
}
