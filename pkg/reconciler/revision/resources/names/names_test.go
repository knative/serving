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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/pkg/kmeta"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

func TestNamer(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1alpha1.Revision
		f    func(kmeta.Accessor) string
		want string
	}{{
		name: "Deployment too long",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: strings.Repeat("f", 63),
			},
		},
		f:    Deployment,
		want: "fffffffffffffffffffffffffffffffd79284ec10a12ef1b3cc73b212018b89",
	}, {
		name: "Deployment long enough",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: strings.Repeat("f", 52),
			},
		},
		f:    Deployment,
		want: strings.Repeat("f", 52) + "-deployment",
	}, {
		name: "Deployment",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
		},
		f:    Deployment,
		want: "foo-deployment",
	}, {
		name: "ImageCache, barely fits",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: strings.Repeat("u", 57),
			},
		},
		f:    ImageCache,
		want: strings.Repeat("u", 57) + "-cache",
	}, {
		name: "ImageCache, already too long",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: strings.Repeat("u", 63),
			},
		},
		f:    ImageCache,
		want: "uuuuuuuuuuuuuuuuuuuuuuuuuuuuuuuf57130621e0118c593bdee629d7976fa",
	}, {
		name: "ImageCache",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
		},
		f:    ImageCache,
		want: "foo-cache",
	}, {
		name: "KPA",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Name: "baz",
			},
		},
		f:    KPA,
		want: "baz",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.f(test.rev)
			if got != test.want {
				t.Errorf("%s() = %v, wanted %v", test.name, got, test.want)
			}
		})
	}
}
