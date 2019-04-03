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

package resources

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMakeLabels(t *testing.T) {
	tests := []struct {
		name string
		obj  *metav1.ObjectMeta
		add  map[string]string
		want map[string]string
	}{{
		name: "nil labels",
		obj:  &metav1.ObjectMeta{},
		want: map[string]string{},
	}, {
		name: "no labels",
		obj: &metav1.ObjectMeta{
			Labels: map[string]string{},
		},
		want: map[string]string{},
	}, {
		name: "no labels, add",
		obj: &metav1.ObjectMeta{
			Labels: map[string]string{},
		},
		add:  map[string]string{"wish": "you", "were": "here"},
		want: map[string]string{"wish": "you", "were": "here"},
	}, {
		name: "pass through labels",
		obj: &metav1.ObjectMeta{
			Labels: map[string]string{"the-dark": "side"},
		},
		want: map[string]string{"the-dark": "side"},
	}, {
		name: "all together now",
		obj: &metav1.ObjectMeta{
			Labels: map[string]string{"another": "brick"},
		},
		add:  map[string]string{"in": "the-wall"},
		want: map[string]string{"in": "the-wall", "another": "brick"},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MakeLabels(test.obj, test.add)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("MakeLabels (-want, +got) = %v", diff)
			}
		})
	}
}

func TestMakeAnnotations(t *testing.T) {
	tests := []struct {
		name   string
		obj    *metav1.ObjectMeta
		filter func(string) bool
		want   map[string]string
	}{{
		name: "nil annotations",
		obj:  &metav1.ObjectMeta{},
		want: map[string]string{},
	}, {
		name: "no annotations",
		obj: &metav1.ObjectMeta{
			Annotations: map[string]string{},
		},
		want: map[string]string{},
	}, {
		name: "no annotations, filterfunc",
		obj: &metav1.ObjectMeta{
			Annotations: map[string]string{},
		},
		filter: func(string) bool { return false },
		want:   map[string]string{},
	}, {
		name: "pass through annotations",
		obj: &metav1.ObjectMeta{
			Annotations: map[string]string{"the-dark": "side"},
		},
		want: map[string]string{"the-dark": "side"},
	}, {
		name: "filter all",
		obj: &metav1.ObjectMeta{
			Annotations: map[string]string{"the-dark": "side", "of-ther": "moon"},
		},
		filter: func(string) bool { return true },
		want:   map[string]string{},
	}, {
		name: "all together now",
		obj: &metav1.ObjectMeta{
			Annotations: map[string]string{"another": "brick", "in": "the-wall"},
		},
		filter: func(s string) bool { return s == "in" },
		want:   map[string]string{"another": "brick"},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MakeAnnotations(test.obj, test.filter)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("MakeAnnotations (-want, +got) = %v", diff)
			}
		})
	}
}
