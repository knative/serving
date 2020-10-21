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
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
)

func TestDomainMappingValidation(t *testing.T) {
	tests := []struct {
		name string
		dm   *DomainMapping
		want *apis.FieldError
	}{{
		name: "uses GenerateName rather than Name",
		want: apis.ErrMissingField("metadata.name"),
		dm: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "cant-use-this",
			},
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Name: "some-name",
				},
			},
		},
	}, {
		name: "missing ref name",
		want: apis.ErrMissingField("spec.ref.name"),
		dm: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name: "missing-ref",
			},
		},
	}, {
		name: "ref in wrong namespace",
		want: &apis.FieldError{
			Paths:   []string{"spec.ref.namespace"},
			Message: "Ref namespace must be empty or equal to the domain mapping namespace \"good-namespace\"",
		},
		dm: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wrong-ref-ns",
				Namespace: "good-namespace",
			},
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Name:      "some-name",
					Namespace: "bad-namespace",
				},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			got := test.dm.Validate(ctx)
			if !cmp.Equal(test.want.Error(), got.Error()) {
				t.Errorf("Validate (-want, +got):\n%s", cmp.Diff(test.want.Error(), got.Error()))
			}
		})
	}
}
