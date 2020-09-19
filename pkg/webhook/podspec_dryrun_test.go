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

package webhook

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	rest "k8s.io/client-go/rest"
	ktesting "k8s.io/client-go/testing"

	"knative.dev/pkg/apis"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	"knative.dev/pkg/logging"
	logtesting "knative.dev/pkg/logging/testing"
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

	om := metav1.ObjectMeta{
		Name:      "valid",
		Namespace: "foo",
		Annotations: map[string]string{
			"features.knative.dev/podspec-dryrun": "enabled",
		},
	}

	tests := []struct {
		name          string
		s             *v1.Service
		want          string
		modifyContext func(context.Context)
		podInterface  func(client rest.Interface, namespace string) podInterface
	}{{
		name:         "valid run latest",
		podInterface: newTestPods,
		s: &v1.Service{
			ObjectMeta: om,
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
		modifyContext: nil,
	}, {
		name:         "dryrun fail",
		podInterface: newFailTestPods,
		s: &v1.Service{
			ObjectMeta: om,
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
		want:          "dry run failed with fail-reason: spec.template",
		modifyContext: failKubeCalls,
	}, {
		name:         "no template found",
		podInterface: newTestPods,
		s: &v1.Service{
			ObjectMeta: om,
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
		modifyContext: nil,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			newCreateWithOptions = test.podInterface
			ctx, _ := fakekubeclient.With(context.Background())
			if test.modifyContext != nil {
				test.modifyContext(ctx)
			}
			logger := logtesting.TestLogger(t)
			ctx = logging.WithLogger(apis.WithDryRun(ctx), logger)

			unstruct := &unstructured.Unstructured{}
			content, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(test.s)
			unstruct.SetUnstructuredContent(content)

			got := ValidateService(ctx, unstruct)
			if got == nil {
				if test.want != "" {
					t.Errorf("Validate got='%v', want='%v'", got, test.want)
				}
			} else if test.want != got.Error() {
				t.Errorf("Validate got='%v', want='%v'", got.Error(), test.want)
			}
		})

	}
}

func TestConfigurationValidation(t *testing.T) {
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

	om := metav1.ObjectMeta{
		Name:      "valid",
		Namespace: "foo",
		Annotations: map[string]string{
			"features.knative.dev/podspec-dryrun": "enabled",
		},
	}

	tests := []struct {
		name          string
		c             *v1.Configuration
		want          string
		modifyContext func(context.Context)
		podInterface  func(client rest.Interface, namespace string) podInterface
	}{{
		name:         "valid run latest",
		podInterface: newTestPods,
		c: &v1.Configuration{
			ObjectMeta: om,
			Spec:       goodConfigSpec,
		},
		modifyContext: nil,
	}, {
		name:         "dryrun fail",
		podInterface: newFailTestPods,
		c: &v1.Configuration{
			ObjectMeta: om,
			Spec:       goodConfigSpec,
		},
		want:          "dry run failed with fail-reason: spec.template",
		modifyContext: failKubeCalls,
	}, {
		name:         "skip service owned",
		podInterface: newFailTestPods,
		c: &v1.Configuration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid",
				Namespace: "foo",
				Labels: map[string]string{
					"serving.knative.dev/service": "skip-me",
				},
				Annotations: map[string]string{
					"features.knative.dev/podspec-dryrun": "enabled",
				},
			},
			Spec: goodConfigSpec,
		},
		modifyContext: nil,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			newCreateWithOptions = test.podInterface
			ctx, _ := fakekubeclient.With(context.Background())
			if test.modifyContext != nil {
				test.modifyContext(ctx)
			}
			logger := logtesting.TestLogger(t)
			ctx = logging.WithLogger(apis.WithDryRun(ctx), logger)

			unstruct := &unstructured.Unstructured{}
			content, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(test.c)
			unstruct.SetUnstructuredContent(content)

			got := ValidateConfiguration(ctx, unstruct)
			if got == nil {
				if test.want != "" {
					t.Errorf("Validate got='%v', want='%v'", got, test.want)
				}
			} else if test.want != got.Error() {
				t.Errorf("Validate got='%v', want='%v'", got.Error(), test.want)
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
