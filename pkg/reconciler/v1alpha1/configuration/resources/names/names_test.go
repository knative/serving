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

package names

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

func TestNamer(t *testing.T) {
	tests := []struct {
		name          string
		configuration *v1alpha1.Configuration
		f             func(*v1alpha1.Configuration) string
		want          string
	}{{
		name: "Revision(42)",
		configuration: &v1alpha1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "default",
			},
			Spec: v1alpha1.ConfigurationSpec{
				Generation: 42,
			},
		},
		f:    Revision,
		want: "foo-00042",
	}, {
		name: "Revision(31)",
		configuration: &v1alpha1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bar",
				Namespace: "default",
			},
			Spec: v1alpha1.ConfigurationSpec{
				Generation: 31,
			},
		},
		f:    Revision,
		want: "bar-00031",
	}, {
		name: "Build(no build)",
		configuration: &v1alpha1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "baz",
				Namespace: "default",
			},
			Spec: v1alpha1.ConfigurationSpec{
				Generation: 16,
			},
		},
		f:    Build,
		want: "",
	}, {
		name: "Build(999, with build)",
		configuration: &v1alpha1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "blah",
				Namespace: "default",
			},
			Spec: v1alpha1.ConfigurationSpec{
				Generation: 999,
				Build: &v1alpha1.RawExtension{
					BuildSpec: &buildv1alpha1.BuildSpec{},
				},
			},
		},
		f:    Build,
		want: "blah-00999",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.f(test.configuration)
			if got != test.want {
				t.Errorf("%s() = %v, wanted %v", test.name, got, test.want)
			}
		})
	}
}
