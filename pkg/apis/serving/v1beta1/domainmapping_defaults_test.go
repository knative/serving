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

package v1beta1

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/serving/pkg/apis/serving"
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

func TestDomainMappingUserInfo(t *testing.T) {
	const (
		u1 = "oveja@knative.dev"
		u2 = "cabra@knative.dev"
		u3 = "vaca@knative.dev"
	)
	withUserAnns := func(u1, u2 string, s *DomainMapping) *DomainMapping {
		a := s.GetAnnotations()
		if a == nil {
			a = map[string]string{}
			s.SetAnnotations(a)
		}
		a[serving.CreatorAnnotation] = u1
		a[serving.UpdaterAnnotation] = u2
		return s
	}
	tests := []struct {
		name     string
		user     string
		this     *DomainMapping
		prev     *DomainMapping
		wantAnns map[string]string
	}{{
		name: "create-new",
		user: u1,
		this: &DomainMapping{},
		prev: nil,
		wantAnns: map[string]string{
			serving.CreatorAnnotation: u1,
			serving.UpdaterAnnotation: u1,
		},
	}, {
		name: "update-no-diff-new-object",
		user: u2,
		this: withUserAnns(u1, u1, &DomainMapping{}),
		prev: withUserAnns(u1, u1, &DomainMapping{}),
		wantAnns: map[string]string{
			serving.CreatorAnnotation: u1,
			serving.UpdaterAnnotation: u1,
		},
	}, {
		name: "update-diff-old-object",
		user: u2,
		this: &DomainMapping{
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Name: "new",
				},
			},
		},
		prev: &DomainMapping{
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Name: "old",
				},
			},
		},
		wantAnns: map[string]string{
			serving.UpdaterAnnotation: u2,
		},
	}, {
		name: "update-diff-new-object",
		user: u3,
		this: withUserAnns(u1, u2, &DomainMapping{
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Name: "new",
				},
			},
		}),
		prev: withUserAnns(u1, u2, &DomainMapping{
			Spec: DomainMappingSpec{
				Ref: duckv1.KReference{
					Name: "old",
				},
			},
		}),
		wantAnns: map[string]string{
			serving.CreatorAnnotation: u1,
			serving.UpdaterAnnotation: u3,
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := apis.WithUserInfo(context.Background(), &authv1.UserInfo{
				Username: test.user,
			})
			if test.prev != nil {
				ctx = apis.WithinUpdate(ctx, test.prev)
				test.prev.SetDefaults(ctx)
			}
			test.this.SetDefaults(ctx)
			if got, want := test.this.GetAnnotations(), test.wantAnns; !cmp.Equal(got, want) {
				t.Errorf("Annotations = %v, want: %v, diff (-got, +want): %s", got, want, cmp.Diff(got, want))
			}
		})
	}
}
