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
	"knative.dev/serving/pkg/apis/serving"
)

func TestDomainMappingValidation(t *testing.T) {
	tests := []struct {
		name string
		dm   *DomainMapping
		want *apis.FieldError
	}{{
		name: "invalid name",
		want: apis.ErrGeneric("invalid name \"invalid\": name: Invalid value: \"invalid\": should be a domain with at least two segments separated by dots", "metadata.name"),
		dm: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid",
				Namespace: "ns",
			},
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Name:       "some-name.example.com",
					APIVersion: "serving.knative.dev/v1",
					Kind:       "Service",
					Namespace:  "ns",
				},
			},
		},
	}, {
		name: "uses GenerateName rather than Name",
		want: apis.ErrDisallowedFields("metadata.generateName").Also(
			apis.ErrGeneric("invalid name \"\": name: Required value", "metadata.name")),
		dm: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "cant-use-this",
				Namespace:    "ns",
			},
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Name:       "some-name.example.com",
					APIVersion: "serving.knative.dev/v1",
					Kind:       "Service",
					Namespace:  "ns",
				},
			},
		},
	}, {
		name: "ref in wrong namespace",
		want: &apis.FieldError{
			Paths:   []string{"spec.ref.namespace"},
			Details: `parent namespace: "good-namespace" does not match ref: "bad-namespace"`,
			Message: `mismatched namespaces`,
		},
		dm: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wrong-ref-ns.example.com",
				Namespace: "good-namespace",
			},
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Name:       "some-name",
					Namespace:  "bad-namespace",
					APIVersion: "serving.knative.dev/v1",
					Kind:       "Service",
				},
			},
		},
	}, {
		name: "ref missing Kind",
		want: apis.ErrMissingField("spec.ref.kind"),
		dm: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wrong-kind.example.com",
				Namespace: "ns",
			},
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Name:       "some-name",
					Namespace:  "ns",
					APIVersion: "serving.knative.dev/v1",
				},
			},
		},
	}, {
		name: "ref missing ApiVersion",
		want: apis.ErrMissingField("spec.ref.apiVersion"),
		dm: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wrong-version.example.com",
				Namespace: "ns",
			},
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Name:      "some-name",
					Namespace: "ns",
					Kind:      "Service",
				},
			},
		},
	}, {
		name: "cluster local domain name",
		want: apis.ErrGeneric("invalid name \"notallowed.svc.cluster.local\": must not be a subdomain of cluster local domain \"cluster.local\"", "metadata.name"),
		dm: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "notallowed.svc.cluster.local",
				Namespace: "ns",
			},
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Name:       "some-name",
					Namespace:  "ns",
					Kind:       "Service",
					APIVersion: "serving.knative.dev/v1",
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

func TestDomainMappingAnnotationUpdate(t *testing.T) {
	const (
		u1 = "oveja@knative.dev"
		u2 = "cabra@knative.dev"
		u3 = "vaca@knative.dev"
	)
	spec := func(name string) DomainMappingSpec {
		return DomainMappingSpec{
			Ref: duckv1.KReference{
				Name:       name,
				Namespace:  "ns",
				Kind:       "Service",
				APIVersion: "serving.knative.dev/v1",
			},
		}
	}
	tests := []struct {
		name string
		prev *DomainMapping
		this *DomainMapping
		want *apis.FieldError
	}{{
		name: "update creator annotation",
		this: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid.example.com",
				Namespace: "ns",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u2,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: spec("old"),
		},
		prev: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid.example.com",
				Namespace: "ns",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: spec("old"),
		},
		want: (&apis.FieldError{Message: "annotation value is immutable",
			Paths: []string{serving.CreatorAnnotation}}).ViaField("metadata.annotations"),
	}, {
		name: "update creator annotation with spec changes",
		this: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid.example.com",
				Namespace: "ns",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u2,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: spec("new"),
		},
		prev: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid.example.com",
				Namespace: "ns",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: spec("old"),
		},
		want: (&apis.FieldError{Message: "annotation value is immutable",
			Paths: []string{serving.CreatorAnnotation}}).ViaField("metadata.annotations"),
	}, {
		name: "update lastModifier annotation without spec changes",
		this: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid.example.com",
				Namespace: "ns",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u2,
				},
			},
			Spec: spec("old"),
		},
		prev: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid.example.com",
				Namespace: "ns",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: spec("old"),
		},
		want: apis.ErrInvalidValue(u2, serving.UpdaterAnnotation).ViaField("metadata.annotations"),
	}, {
		name: "update lastModifier annotation with spec changes",
		this: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid.example.com",
				Namespace: "ns",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u3,
				},
			},
			Spec: spec("new"),
		},
		prev: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid.example.com",
				Namespace: "ns",
				Annotations: map[string]string{
					serving.CreatorAnnotation: u1,
					serving.UpdaterAnnotation: u1,
				},
			},
			Spec: spec("old"),
		},
		want: nil,
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = apis.WithinUpdate(ctx, test.prev)
			if diff := cmp.Diff(test.want.Error(), test.this.Validate(ctx).Error()); diff != "" {
				t.Error("Validate (-want, +got) =", diff)
			}
		})
	}
}
