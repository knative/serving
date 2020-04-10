/*
Copyright 2020 The Knative Authors.

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

package validation

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"

	"knative.dev/pkg/apis"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	"knative.dev/pkg/ptr"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

func TestExtraServiceValidation(t *testing.T) {
	goodConfigSpec := v1.ConfigurationSpec{
		Template: v1.RevisionTemplateSpec{
			Spec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "busybox",
					}},
				},
			},
		},
	}

	tests := []struct {
		name          string
		s             *v1.Service
		want          *apis.FieldError
		modifyContext func(context.Context)
	}{{
		name: "valid run latest",
		s: &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid",
				Namespace: "foo",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: goodConfigSpec,
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						LatestRevision: ptr.Bool(true),
						Percent:        ptr.Int64(100),
					}},
				},
			},
		},
		want:          nil,
		modifyContext: nil,
	}, {
		name: "dryrun fail",
		s: &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid",
				Namespace: "foo",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: goodConfigSpec,
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						LatestRevision: ptr.Bool(true),
						Percent:        ptr.Int64(100),
					}},
				},
			},
		},
		want:          apis.ErrGeneric("podSpec dry run failed", "kubeclient error"),
		modifyContext: failKubeCalls,
	}, {
		name: "dryrun not supported succeeds",
		s: &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid",
				Namespace: "foo",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: goodConfigSpec,
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						LatestRevision: ptr.Bool(true),
						Percent:        ptr.Int64(100),
					}},
				},
			},
		},
		want:          nil, // Not supported fails soft
		modifyContext: dryRunNotSupported,
	}, {
		name: "no template found",
		s: &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid",
				Namespace: "foo",
			},
			Spec: v1.ServiceSpec{
				ConfigurationSpec: v1.ConfigurationSpec{}, // Empty spec
				RouteSpec: v1.RouteSpec{
					Traffic: []v1.TrafficTarget{{
						LatestRevision: ptr.Bool(true),
						Percent:        ptr.Int64(100),
					}},
				},
			},
		},
		want:          nil,
		modifyContext: nil,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := fakekubeclient.With(context.Background())
			if test.modifyContext != nil {
				test.modifyContext(ctx)
			}

			unstruct := &unstructured.Unstructured{}
			content, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(test.s)
			unstruct.SetUnstructuredContent(content)

			got := ExtraServiceValidation(ctx, unstruct)
			if (test.want != nil || got != nil) && !cmp.Equal(test.want.Error(), got.Error()) {
				t.Errorf("Validate (-want, +got) = %v",
					cmp.Diff(test.want.Error(), got.Error()))
			}
		})
	}
}

func failKubeCalls(ctx context.Context) {
	client := fakekubeclient.Get(ctx)
	client.PrependReactor("*", "*",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("kubeclient error")
		},
	)
}

func dryRunNotSupported(ctx context.Context) {
	client := fakekubeclient.Get(ctx)
	client.PrependReactor("*", "*",
		func(action ktesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("fakekube does not support dry run")
		},
	)
}
