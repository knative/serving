// +build e2e

/*
Copyright 2019 The Knative Authors

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

package runtime

import (
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/knative/pkg/ptr"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/test"
	v1a1test "github.com/knative/serving/test/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// TestMustNotContainerContraints tests that attempting to set unsupported fields or invalid values as
// defined by "MUST NOT" statements from the runtime contract results in a user facing error.
func TestMustNotContainerConstraints(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	testCases := []struct {
		name    string
		options func(s *v1alpha1.Service)
	}{{
		name: "TestArbitraryPortName",
		options: func(s *v1alpha1.Service) {
			s.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{{
				Name:          "arbitrary",
				ContainerPort: 8080,
			}}
		},
	}, {
		name: "TestMountPropagation",
		options: func(s *v1alpha1.Service) {
			propagationMode := corev1.MountPropagationHostToContainer
			s.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{
				Name:             "VolumeMount",
				MountPath:        "/",
				MountPropagation: &propagationMode,
			}}
		},
	}, {
		name: "TestReadinessHTTPProbePort",
		options: func(s *v1alpha1.Service) {
			s.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
				Handler: corev1.Handler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/",
						Port: intstr.FromInt(8888),
					},
				},
			}
		},
	}, {
		name: "TestLivenessHTTPProbePort",
		options: func(s *v1alpha1.Service) {
			s.Spec.Template.Spec.Containers[0].LivenessProbe = &corev1.Probe{
				Handler: corev1.Handler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/",
						Port: intstr.FromInt(8888),
					},
				},
			}
		},
	}, {
		name: "TestReadinessTCPProbePort",
		options: func(s *v1alpha1.Service) {
			s.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
				Handler: corev1.Handler{
					TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(8888)},
				},
			}
		},
	}, {
		name: "TestLivenessTCPProbePort",
		options: func(s *v1alpha1.Service) {
			s.Spec.Template.Spec.Containers[0].LivenessProbe = &corev1.Probe{
				Handler: corev1.Handler{
					TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(8888)},
				},
			}
		},
	}}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			names := test.ResourceNames{
				Service: test.ObjectNameForTest(t),
				Image:   test.Runtime,
			}
			if svc, err := v1a1test.CreateLatestService(t, clients, names, &v1a1test.Options{}, tc.options); err == nil {
				t.Errorf("CreateService = %v, want: error", spew.Sdump(svc))
			}
		})
	}
}

// TestShouldNotContainerContraints tests that attempting to set unsupported fields or invalid values as
// defined by "SHOULD NOT" statements from the runtime contract results in a user facing error.
func TestShouldNotContainerConstraints(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	testCases := []struct {
		name    string
		options func(s *v1alpha1.Service)
	}{{
		name: "TestPoststartHook",
		options: func(s *v1alpha1.Service) {
			lifecycleHandler := &corev1.ExecAction{
				Command: []string{"/bin/sh", "-c", "echo Hello from the post start handler > /usr/share/message"},
			}
			s.Spec.Template.Spec.Containers[0].Lifecycle = &corev1.Lifecycle{
				PostStart: &corev1.Handler{Exec: lifecycleHandler},
			}
		},
	}, {
		name: "TestPrestopHook",
		options: func(s *v1alpha1.Service) {
			lifecycleHandler := &corev1.ExecAction{
				Command: []string{"/bin/sh", "-c", "echo Hello from the pre stop handler > /usr/share/message"},
			}
			s.Spec.Template.Spec.Containers[0].Lifecycle = &corev1.Lifecycle{
				PreStop: &corev1.Handler{Exec: lifecycleHandler},
			}
		},
	}, {
		name: "TestMultiplePorts",
		options: func(s *v1alpha1.Service) {
			s.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
				{ContainerPort: 80},
				{ContainerPort: 81},
			}
		},
	}, {
		name: "TestHostPort",
		options: func(s *v1alpha1.Service) {
			s.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{{
				ContainerPort: 8081,
				HostPort:      80,
			}}
		},
	}, {
		name: "TestStdin",
		options: func(s *v1alpha1.Service) {
			s.Spec.Template.Spec.Containers[0].Stdin = true
		},
	}, {
		name: "TestStdinOnce",
		options: func(s *v1alpha1.Service) {
			s.Spec.Template.Spec.Containers[0].StdinOnce = true
		},
	}, {
		name: "TestTTY",
		options: func(s *v1alpha1.Service) {
			s.Spec.Template.Spec.Containers[0].TTY = true
		},
	}, {
		name: "TestInvalidUID",
		options: func(s *v1alpha1.Service) {
			s.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
				RunAsUser: ptr.Int64(-10),
			}
		},
	}}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			names := test.ResourceNames{
				Service: test.ObjectNameForTest(t),
				Image:   test.Runtime,
			}
			if svc, err := v1a1test.CreateLatestService(t, clients, names, &v1a1test.Options{}, tc.options); err == nil {
				t.Errorf("CreateLatestService = %v, want: error", spew.Sdump(svc))
			}
		})
	}
}
