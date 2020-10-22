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
		name: "uses GenerateName rather than Name",
		want: apis.ErrDisallowedFields("metadata.generateName"),
		dm: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "cant-use-this",
				Namespace:    "ns",
			},
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Name:       "some-name",
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
				Name:      "wrong-ref-ns",
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
		name: "ref wrong Kind",
		want: apis.ErrGeneric(`must be "Service"`, "spec.ref.kind"),
		dm: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wrong-kind",
				Namespace: "ns",
			},
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Name:       "some-name",
					Namespace:  "ns",
					APIVersion: "serving.knative.dev/v1",
					Kind:       "BadService",
				},
			},
		},
	}, {
		name: "ref wrong ApiVersion",
		want: apis.ErrGeneric(`must be "serving.knative.dev/v1"`, "spec.ref.apiVersion"),
		dm: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wrong-version",
				Namespace: "ns",
			},
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Name:       "some-name",
					Namespace:  "ns",
					APIVersion: "bad.version/v1",
					Kind:       "Service",
				},
			},
		},
	}, {
		name: "ref name not valid DNS subdomain",
		want: apis.ErrInvalidValue(
			"not a DNS 1035 label prefix: [a DNS-1035 label must consist of lower case alphanumeric characters or '-', start with an alphabetic character, and end with an alphanumeric character (e.g. 'my-name',  or 'abc-123', regex used for validation is '[a-z]([-a-z0-9]*[a-z0-9])?')]",
			"spec.ref.name"),
		dm: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wrong-version",
				Namespace: "ns",
			},
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Name:       "this is not valid",
					Namespace:  "ns",
					APIVersion: "serving.knative.dev/v1",
					Kind:       "Service",
				},
			},
		},
	}, {
		name: "ref namespace not valid",
		want: apis.ErrInvalidValue("not a valid namespace: [a DNS-1123 label must consist of lower case alphanumeric characters or '-', and must start and end with an alphanumeric character (e.g. 'my-name',  or '123-abc', regex used for validation is '[a-z0-9]([-a-z0-9]*[a-z0-9])?')]", "spec.ref.namespace"),
		dm: &DomainMapping{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wrong-version",
				Namespace: "dots.not.allowed",
			},
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Name:       "name",
					APIVersion: "serving.knative.dev/v1",
					Namespace:  "dots.not.allowed",
					Kind:       "Service",
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
				Name:      "valid",
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
				Name:      "valid",
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
				Name:      "valid",
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
				Name:      "valid",
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
				Name:      "valid",
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
				Name:      "valid",
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
				Name:      "valid",
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
				Name:      "valid",
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
