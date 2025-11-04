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

package resources

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"knative.dev/serving/pkg/apis/autoscaling"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

func TestMakeLabels(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1.Revision
		want map[string]string
	}{{
		name: "no user labels",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
		},
		want: map[string]string{
			serving.RevisionLabelKey: "bar",
			serving.RevisionUID:      "1234",
			AppLabelKey:              "bar",
		},
	}, {
		name: "propagate user labels",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels: map[string]string{
					"ooga":    "booga",
					"unicorn": "rainbows",
				},
			},
		},
		want: map[string]string{
			serving.RevisionLabelKey: "bar",
			serving.RevisionUID:      "1234",
			AppLabelKey:              "bar",
			"ooga":                   "booga",
			"unicorn":                "rainbows",
		},
	}, {
		name: "override app label key",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels: map[string]string{
					AppLabelKey: "my-app-override",
				},
			},
		},
		want: map[string]string{
			serving.RevisionLabelKey: "bar",
			serving.RevisionUID:      "1234",
			AppLabelKey:              "my-app-override",
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := makeLabels(test.rev)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Error("makeLabels (-want, +got) =", diff)
			}

			wantSelector := &metav1.LabelSelector{
				MatchLabels: map[string]string{serving.RevisionUID: "1234"},
			}
			gotSelector := makeSelector(test.rev)
			if diff := cmp.Diff(wantSelector, gotSelector); diff != "" {
				t.Error("makeLabels (-want, +got) =", diff)
			}
		})
	}
}

func TestMakeAnnotations(t *testing.T) {
	type buildFuncs []func(*v1.Revision) map[string]string

	tests := []struct {
		name       string
		buildFuncs buildFuncs
		revAnn     map[string]string
		want       map[string]string
	}{{
		name: "no user annotations",
		buildFuncs: buildFuncs{
			deploymentAnnotations,
			imageCacheAnnotations,
			podAutoscalerAnnotations,
			podAnnotations,
		},
		revAnn: map[string]string{},
		want:   map[string]string{},
	}, {
		name: "excluded annotations",
		buildFuncs: buildFuncs{
			deploymentAnnotations,
			imageCacheAnnotations,
			podAutoscalerAnnotations,
			podAnnotations,
		},
		revAnn: map[string]string{
			serving.RoutingStateModifiedAnnotationKey: "exclude me",
			"keep": "keep me",
		},
		want: map[string]string{"keep": "keep me"},
	}, {
		name: "exclude autoscaling annotations",
		buildFuncs: buildFuncs{
			deploymentAnnotations,
			imageCacheAnnotations,
			podAnnotations,
		},
		revAnn: map[string]string{
			autoscaling.MinScaleAnnotationKey: "1",
			"keep":                            "keep me",
		},
		want: map[string]string{"keep": "keep me"},
	}, {
		name: "include autoscaling annotations",
		buildFuncs: buildFuncs{
			podAutoscalerAnnotations,
		},
		revAnn: map[string]string{
			autoscaling.MinScaleAnnotationKey: "1",
			"keep":                            "keep me",
		},
		want: map[string]string{
			autoscaling.MinScaleAnnotationKey: "1",
			"keep":                            "keep me",
		},
	}}

	for _, test := range tests {
		for _, buildFunc := range test.buildFuncs {
			funcName := runtime.FuncForPC(reflect.ValueOf(buildFunc).Pointer()).Name()
			i := strings.LastIndex(funcName, ".")
			funcName = funcName[i+1:]

			testName := fmt.Sprint(test.name, " ", funcName)

			t.Run(testName, func(t *testing.T) {
				rev := &v1.Revision{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:   "foo",
						Name:        "bar",
						Annotations: test.revAnn,
					},
				}

				got := buildFunc(rev)

				if diff := cmp.Diff(test.want, got); diff != "" {
					t.Errorf("%s(-want, +got) = %s", funcName, diff)
				}
			})
		}
	}
}

func TestMakeAnnotationsForPod(t *testing.T) {
	tests := []struct {
		name            string
		rev             *v1.Revision
		want            map[string]string
		baseAnnotations map[string]string
	}{{
		name: "multiple containers single port with base annotation",
		rev: &v1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"asdf": "fdsa",
				},
			},
			Spec: v1.RevisionSpec{
				PodSpec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "foo",
					}, {
						Name: "bar",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8080,
						}},
					}},
				},
			},
		},
		want: map[string]string{
			"asdf":                         "fdsa",
			DefaultContainerAnnotationName: "bar",
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := podAnnotations(test.rev)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Error("getUserContainerName (-want, +got) =", diff)
			}
		})
	}
}
