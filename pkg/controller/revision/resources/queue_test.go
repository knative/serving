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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/autoscaler"
	"github.com/knative/serving/pkg/controller/revision/config"
	"github.com/knative/serving/pkg/logging"
)

var boolTrue = true

func TestMakequeueContainer(t *testing.T) {
	tests := []struct {
		name string
		rev  *v1alpha1.Revision
		lc   *logging.Config
		ac   *autoscaler.Config
		cc   *config.Controller
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
				ConcurrencyModel: "Single",
			},
		},
		lc: &logging.Config{},
		ac: &autoscaler.Config{},
		cc: &config.Controller{},
		want: &corev1.Container{
			// These are effectively constant
			Name:           queueContainerName,
			Resources:      queueResources,
			Ports:          queuePorts,
			Lifecycle:      queueLifecycle,
			ReadinessProbe: queueReadinessProbe,
			// These changed based on the Revision and configs passed in.
			Args: []string{"-concurrencyQuantumOfTime=0s", "-concurrencyModel=Single"},
			Env: []corev1.EnvVar{{
				Name:  "SERVING_NAMESPACE",
				Value: "foo", // matches namespace
			}, {
				Name: "SERVING_CONFIGURATION",
				// No OwnerReference
			}, {
				Name:  "SERVING_REVISION",
				Value: "bar", // matches name
			}, {
				Name:  "SERVING_AUTOSCALER",
				Value: "autoscaler", // no autoscaler configured.
			}, {
				Name:  "SERVING_AUTOSCALER_PORT",
				Value: "8080",
			}, {
				Name: "SERVING_POD",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
				},
			}, {
				Name: "SERVING_LOGGING_CONFIG",
				// No logging configuration
			}, {
				Name: "SERVING_LOGGING_LEVEL",
				// No logging level
			}},
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
				ConcurrencyModel: "Single",
			},
		},
		lc: &logging.Config{},
		ac: &autoscaler.Config{},
		cc: &config.Controller{
			QueueSidecarImage: "alpine",
		},
		want: &corev1.Container{
			// These are effectively constant
			Name:           queueContainerName,
			Resources:      queueResources,
			Ports:          queuePorts,
			Lifecycle:      queueLifecycle,
			ReadinessProbe: queueReadinessProbe,
			// These changed based on the Revision and configs passed in.
			Image: "alpine",
			Args:  []string{"-concurrencyQuantumOfTime=0s", "-concurrencyModel=Single"},
			Env: []corev1.EnvVar{{
				Name:  "SERVING_NAMESPACE",
				Value: "foo", // matches namespace
			}, {
				Name: "SERVING_CONFIGURATION",
				// No OwnerReference
			}, {
				Name:  "SERVING_REVISION",
				Value: "bar", // matches name
			}, {
				Name:  "SERVING_AUTOSCALER",
				Value: "autoscaler", // no autoscaler configured.
			}, {
				Name:  "SERVING_AUTOSCALER_PORT",
				Value: "8080",
			}, {
				Name: "SERVING_POD",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
				},
			}, {
				Name: "SERVING_LOGGING_CONFIG",
				// No logging configuration
			}, {
				Name: "SERVING_LOGGING_LEVEL",
				// No logging level
			}},
		},
	}, {
		name: "config owner as env var, multi-concurrency",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "baz",
				Name:      "blah",
				UID:       "1234",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         v1alpha1.SchemeGroupVersion.String(),
					Kind:               "Configuration",
					Name:               "the-parent-config-name",
					Controller:         &boolTrue,
					BlockOwnerDeletion: &boolTrue,
				}},
			},
			Spec: v1alpha1.RevisionSpec{
				ConcurrencyModel: "Multi",
			},
		},
		lc: &logging.Config{},
		ac: &autoscaler.Config{},
		cc: &config.Controller{},
		want: &corev1.Container{
			// These are effectively constant
			Name:           queueContainerName,
			Resources:      queueResources,
			Ports:          queuePorts,
			Lifecycle:      queueLifecycle,
			ReadinessProbe: queueReadinessProbe,
			// These changed based on the Revision and configs passed in.
			Args: []string{"-concurrencyQuantumOfTime=0s", "-concurrencyModel=Multi"},
			Env: []corev1.EnvVar{{
				Name:  "SERVING_NAMESPACE",
				Value: "baz", // matches namespace
			}, {
				Name:  "SERVING_CONFIGURATION",
				Value: "the-parent-config-name",
			}, {
				Name:  "SERVING_REVISION",
				Value: "blah", // matches name
			}, {
				Name:  "SERVING_AUTOSCALER",
				Value: "autoscaler", // no autoscaler configured.
			}, {
				Name:  "SERVING_AUTOSCALER_PORT",
				Value: "8080",
			}, {
				Name: "SERVING_POD",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
				},
			}, {
				Name: "SERVING_LOGGING_CONFIG",
				// No logging configuration
			}, {
				Name: "SERVING_LOGGING_LEVEL",
				// No logging level
			}},
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
				ConcurrencyModel: "Multi",
			},
		},
		lc: &logging.Config{
			LoggingConfig: "The logging configuration goes here",
			LoggingLevel: map[string]string{
				"queueproxy": "error",
			},
		},
		ac: &autoscaler.Config{},
		cc: &config.Controller{},
		want: &corev1.Container{
			// These are effectively constant
			Name:           queueContainerName,
			Resources:      queueResources,
			Ports:          queuePorts,
			Lifecycle:      queueLifecycle,
			ReadinessProbe: queueReadinessProbe,
			// These changed based on the Revision and configs passed in.
			Args: []string{"-concurrencyQuantumOfTime=0s", "-concurrencyModel=Multi"},
			Env: []corev1.EnvVar{{
				Name:  "SERVING_NAMESPACE",
				Value: "log", // matches namespace
			}, {
				Name: "SERVING_CONFIGURATION",
				// No Configuration owner.
			}, {
				Name:  "SERVING_REVISION",
				Value: "this", // matches name
			}, {
				Name:  "SERVING_AUTOSCALER",
				Value: "autoscaler", // no autoscaler configured.
			}, {
				Name:  "SERVING_AUTOSCALER_PORT",
				Value: "8080",
			}, {
				Name: "SERVING_POD",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
				},
			}, {
				Name:  "SERVING_LOGGING_CONFIG",
				Value: "The logging configuration goes here", // from logging config
			}, {
				Name:  "SERVING_LOGGING_LEVEL",
				Value: "error", // from logging config
			}},
		},
	}, {
		name: "autoscaler configuration options",
		rev: &v1alpha1.Revision{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "what-does-the",
				Name:      "autoscaler-do",
				UID:       "4321",
			},
			Spec: v1alpha1.RevisionSpec{
				ConcurrencyModel: "Multi",
			},
		},
		lc: &logging.Config{},
		ac: &autoscaler.Config{
			ConcurrencyQuantumOfTime: 12 * time.Minute,
		},
		cc: &config.Controller{
			AutoscalerImage: "ubuntu:xenial",
		},
		want: &corev1.Container{
			// These are effectively constant
			Name:           queueContainerName,
			Resources:      queueResources,
			Ports:          queuePorts,
			Lifecycle:      queueLifecycle,
			ReadinessProbe: queueReadinessProbe,
			// These changed based on the Revision and configs passed in.
			Args: []string{"-concurrencyQuantumOfTime=12m0s", "-concurrencyModel=Multi"},
			Env: []corev1.EnvVar{{
				Name:  "SERVING_NAMESPACE",
				Value: "what-does-the", // matches namespace
			}, {
				Name: "SERVING_CONFIGURATION",
				// No Configuration owner.
			}, {
				Name:  "SERVING_REVISION",
				Value: "autoscaler-do", // matches name
			}, {
				Name:  "SERVING_AUTOSCALER",
				Value: "autoscaler-do-autoscaler",
			}, {
				Name:  "SERVING_AUTOSCALER_PORT",
				Value: "8080",
			}, {
				Name: "SERVING_POD",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
				},
			}, {
				Name: "SERVING_LOGGING_CONFIG",
				// No logging config
			}, {
				Name: "SERVING_LOGGING_LEVEL",
				// No logging config
			}},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := makequeueContainer(test.rev, test.lc, test.ac, test.cc)
			if diff := cmp.Diff(test.want, got, cmpopts.IgnoreUnexported(resource.Quantity{})); diff != "" {
				t.Errorf("makequeueContainer (-want, +got) = %v", diff)
			}
		})
	}
}
