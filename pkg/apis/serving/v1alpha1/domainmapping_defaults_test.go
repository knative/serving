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
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	duckv1 "knative.dev/pkg/apis/duck/v1"
)

func TestDomainMappingDefaulting(t *testing.T) {
	tests := []struct {
		name    string
		in, out *DomainMapping
	}{{
		name: "empty",
		in:   &DomainMapping{},
		out:  &DomainMapping{},
	}, {
		name: "empty ref namespace",
		in: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "some-namespace",
			},
		},
		out: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "some-namespace",
			},
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Namespace: "some-namespace",
				},
			},
		},
	}, {
		name: "explicit ref namespace",
		in: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "some-namespace",
			},
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Namespace: "explicit-namespace",
				},
			},
		},
		out: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "some-namespace",
			},
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Namespace: "explicit-namespace",
				},
			},
		},
	}}

	for _, test := range tests {
		ctx := context.Background()

		test.in.SetDefaults(ctx)
		if !cmp.Equal(test.out, test.in) {
			t.Errorf("SetDefaults (-want, +got):\n%s", cmp.Diff(test.out, test.in))
		}
	}
}
