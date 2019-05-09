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
	"sort"
	"strconv"
	"testing"

	"github.com/knative/serving/pkg/resources"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/knative/pkg/logging"
	"github.com/knative/pkg/ptr"
	"github.com/knative/pkg/system"
	_ "github.com/knative/pkg/system/testing"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/deployment"
	"github.com/knative/serving/pkg/metrics"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMakeQueueContainer(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1alpha1.Revision
		lc   *logging.Config
		oc   *metrics.ObservabilityConfig
		ac   *autoscaler.Config
		cc   *deployment.Config
		want *corev1.Container
	}{{
		name: "no owner no autoscaler single",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 1,
					TimeoutSeconds:       ptr.Int64(45),
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{},
		want: &corev1.Container{
			// These are effectively constant
			Name:           QueueContainerName,
			Resources:      queueResources,
			Ports:          append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe: queueReadinessProbe,
			// These changed based on the Revision and configs passed in.
			Env: env(nil),
		},
	}, {
		name: "no owner no autoscaler single",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 1,
					TimeoutSeconds:       ptr.Int64(45),
					PodSpec: v1beta1.PodSpec{
						Containers: []corev1.Container{{
							Ports: []corev1.ContainerPort{{
								ContainerPort: 1955,
								Name:          string(networking.ProtocolH2C),
							}},
						}},
					},
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{
			QueueSidecarImage: "alpine",
		},
		want: &corev1.Container{
			// These are effectively constant
			Name:           QueueContainerName,
			Resources:      queueResources,
			Ports:          append(queueNonServingPorts, queueHTTP2Port),
			ReadinessProbe: queueReadinessProbe,
			// These changed based on the Revision and configs passed in.
			Image: "alpine",
			Env: env(map[string]string{
				"USER_PORT":          "1955",
				"QUEUE_SERVING_PORT": "8013",
			}),
		},
	}, {
		name: "service name in labels",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
				Labels: map[string]string{
					serving.ServiceLabelKey: "svc",
				},
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 1,
					TimeoutSeconds:       ptr.Int64(45),
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{
			QueueSidecarImage: "alpine",
		},
		want: &corev1.Container{
			// These are effectively constant
			Name:           QueueContainerName,
			Resources:      queueResources,
			Ports:          append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe: queueReadinessProbe,
			// These changed based on the Revision and configs passed in.
			Image: "alpine",
			Env: env(map[string]string{
				"SERVING_SERVICE": "svc",
			}),
		}}, {
		name: "config owner as env var, zero concurrency",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "baz",
				Name:      "blah",
				UID:       "1234",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "the-parent-config-name",
					Controller:         ptr.Bool(true),
					BlockOwnerDeletion: ptr.Bool(true),
				}},
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 0,
					TimeoutSeconds:       ptr.Int64(45),
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{},
		want: &corev1.Container{
			// These are effectively constant
			Name:           QueueContainerName,
			Resources:      queueResources,
			Ports:          append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe: queueReadinessProbe,
			// These changed based on the Revision and configs passed in.
			Env: env(map[string]string{
				"CONTAINER_CONCURRENCY": "0",
				"SERVING_CONFIGURATION": "the-parent-config-name",
				"SERVING_NAMESPACE":     "baz",
				"SERVING_REVISION":      "blah",
			}),
		},
	}, {
		name: "logging configuration as env var",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "log",
				Name:      "this",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 0,
					TimeoutSeconds:       ptr.Int64(45),
				},
			},
		},
		lc: &logging.Config{
			LoggingConfig: "The logging configuration goes here",
			LoggingLevel: map[string]zapcore.Level{
				"queueproxy": zapcore.ErrorLevel,
			},
		},
		oc: &metrics.ObservabilityConfig{},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{},
		want: &corev1.Container{
			// These are effectively constant
			Name:           QueueContainerName,
			Resources:      queueResources,
			Ports:          append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe: queueReadinessProbe,
			// These changed based on the Revision and configs passed in.
			Env: env(map[string]string{
				"CONTAINER_CONCURRENCY":  "0",
				"SERVING_LOGGING_CONFIG": "The logging configuration goes here",
				"SERVING_LOGGING_LEVEL":  "error",
				"SERVING_NAMESPACE":      "log",
				"SERVING_REVISION":       "this",
			}),
		},
	}, {
		name: "container concurrency 10",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 10,
					TimeoutSeconds:       ptr.Int64(45),
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{},
		want: &corev1.Container{
			// These are effectively constant
			Name:           QueueContainerName,
			Resources:      queueResources,
			Ports:          append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe: queueReadinessProbe,
			// These changed based on the Revision and configs passed in.
			Env: env(map[string]string{
				"CONTAINER_CONCURRENCY": "10",
			}),
		},
	}, {
		name: "request log as env var",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 0,
					TimeoutSeconds:       ptr.Int64(45),
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{RequestLogTemplate: "test template"},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{},
		want: &corev1.Container{
			// These are effectively constant
			Name:           QueueContainerName,
			Resources:      queueResources,
			Ports:          append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe: queueReadinessProbe,
			// These changed based on the Revision and configs passed in.
			Env: env(map[string]string{
				"CONTAINER_CONCURRENCY":        "0",
				"SERVING_REQUEST_LOG_TEMPLATE": "test template",
			}),
		},
	}, {
		name: "request metrics backend as env var",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "foo",
				Name:      "bar",
				UID:       "1234",
			},
			Spec: v1alpha1.RevisionSpec{
				RevisionSpec: v1beta1.RevisionSpec{
					ContainerConcurrency: 0,
					TimeoutSeconds:       ptr.Int64(45),
				},
			},
		},
		lc: &logging.Config{},
		oc: &metrics.ObservabilityConfig{
			RequestMetricsBackend: "prometheus",
		},
		ac: &autoscaler.Config{},
		cc: &deployment.Config{},
		want: &corev1.Container{
			// These are effectively constant
			Name:           QueueContainerName,
			Resources:      queueResources,
			Ports:          append(queueNonServingPorts, queueHTTPPort),
			ReadinessProbe: queueReadinessProbe,
			// These changed based on the Revision and configs passed in.
			Env: env(map[string]string{
				"CONTAINER_CONCURRENCY":           "0",
				"SERVING_REQUEST_METRICS_BACKEND": "prometheus",
			}),
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := makeQueueContainer(test.rev, test.lc, test.oc, test.ac, test.cc)
			sortEnv(got.Env)
			if diff := cmp.Diff(test.want, got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
				t.Errorf("makeQueueContainer (-want, +got) = %v", diff)
			}
		})
	}
}

var defaultEnv = map[string]string{
	"SERVING_NAMESPACE":               "foo",
	"SERVING_SERVICE":                 "",
	"SERVING_CONFIGURATION":           "",
	"SERVING_REVISION":                "bar",
	"CONTAINER_CONCURRENCY":           "1",
	"REVISION_TIMEOUT_SECONDS":        "45",
	"SERVING_LOGGING_CONFIG":          "",
	"SERVING_LOGGING_LEVEL":           "",
	"SERVING_REQUEST_LOG_TEMPLATE":    "",
	"SERVING_REQUEST_METRICS_BACKEND": "",
	"USER_PORT":                       strconv.Itoa(v1alpha1.DefaultUserPort),
	"SYSTEM_NAMESPACE":                system.Namespace(),
	"QUEUE_SERVING_PORT":              "8012",
}

func env(overrides map[string]string) []corev1.EnvVar {
	values := resources.UnionMaps(defaultEnv, overrides)

	var env []corev1.EnvVar
	for key, value := range values {
		env = append(env, corev1.EnvVar{
			Name:  key,
			Value: value,
		})
	}

	env = append(env, []corev1.EnvVar{
		{
			Name: "SERVING_POD",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
		{
			Name: "SERVING_POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
			},
		},
	}...)

	sortEnv(env)
	return env
}

func sortEnv(envs []corev1.EnvVar) {
	sort.SliceStable(envs, func(i, j int) bool {
		return envs[i].Name < envs[j].Name
	})
}
