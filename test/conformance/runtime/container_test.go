//go:build e2e
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
	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/ptr"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/test"
	testv1 "knative.dev/serving/test/v1"
)

// TestMustNotContainerConstraints tests that attempting to set unsupported fields or invalid values as
// defined by "MUST NOT" statements from the runtime contract results in a user facing error.
func TestMustNotContainerConstraints(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	testCases := []struct {
		name    string
		options func(s *v1.Service)
	}{{
		name: "TestArbitraryPortName",
		options: func(s *v1.Service) {
			s.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{{
				Name: "arbitrary",
			}}
		},
	}, {
		name: "TestMountPropagation",
		options: func(s *v1.Service) {
			propagationMode := corev1.MountPropagationHostToContainer
			s.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{
				Name:             "VolumeMount",
				MountPath:        "/",
				MountPropagation: &propagationMode,
			}}
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
			if svc, err := testv1.CreateService(t, clients, names, tc.options); err == nil {
				t.Errorf("CreateService = %v, want: error", spew.Sdump(svc))
			}
		})
	}
}

// TestShouldNotContainerConstraints tests that attempting to set unsupported fields or invalid values as
// defined by "SHOULD NOT" statements from the runtime contract results in a user facing error.
func TestShouldNotContainerConstraints(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	testCases := []struct {
		name            string
		options         func(s *v1.Service)
		assertIfNoError func(t *testing.T, s *v1.Service)
	}{{
		name: "TestPoststartHook",
		options: func(s *v1.Service) {
			lifecycleHandler := &corev1.ExecAction{
				Command: []string{"/bin/sh", "-c", "echo Hello from the post start handler > /usr/share/message"},
			}
			s.Spec.Template.Spec.Containers[0].Lifecycle = &corev1.Lifecycle{
				PostStart: &corev1.LifecycleHandler{Exec: lifecycleHandler},
			}
		},
		assertIfNoError: func(t *testing.T, svc *v1.Service) {
			lifecycle := svc.Spec.Template.Spec.Containers[0].Lifecycle
			if lifecycle != nil && lifecycle.PostStart != nil {
				t.Error("Expected Lifecycle.PostStart to be pruned")
			}
		},
	}, {
		name: "TestPrestopHook",
		options: func(s *v1.Service) {
			lifecycleHandler := &corev1.ExecAction{
				Command: []string{"/bin/sh", "-c", "echo Hello from the post start handler > /usr/share/message"},
			}
			s.Spec.Template.Spec.Containers[0].Lifecycle = &corev1.Lifecycle{
				PreStop: &corev1.LifecycleHandler{Exec: lifecycleHandler},
			}
		},
		assertIfNoError: func(t *testing.T, svc *v1.Service) {
			lifecycle := svc.Spec.Template.Spec.Containers[0].Lifecycle
			if lifecycle != nil && lifecycle.PreStop != nil {
				t.Error("Expected Lifecycle.Prestop to be pruned")
			}
		},
	}, {
		name: "TestMultiplePorts",
		options: func(s *v1.Service) {
			s.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
				{ContainerPort: 80},
				{ContainerPort: 81},
			}
		},
	}, {
		name: "TestHostPort",
		options: func(s *v1.Service) {
			s.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{{
				HostPort: 80,
			}}
		},
		assertIfNoError: func(t *testing.T, svc *v1.Service) {
			if svc.Spec.Template.Spec.Containers[0].Ports[0].HostPort != 0 {
				t.Error("Expected Containers[].Ports[].HostPort to be pruned")
			}
		},
	}, {
		name: "TestStdin",
		options: func(s *v1.Service) {
			s.Spec.Template.Spec.Containers[0].Stdin = true
		},
		assertIfNoError: func(t *testing.T, svc *v1.Service) {
			if svc.Spec.Template.Spec.Containers[0].Stdin == true {
				t.Error("Expected Stdin to be pruned")
			}
		},
	}, {
		name: "TestStdinOnce",
		options: func(s *v1.Service) {
			s.Spec.Template.Spec.Containers[0].StdinOnce = true
		},
		assertIfNoError: func(t *testing.T, svc *v1.Service) {
			if svc.Spec.Template.Spec.Containers[0].StdinOnce == true {
				t.Error("Expected StdinOnce to be pruned")
			}
		},
	}, {
		name: "TestTTY",
		options: func(s *v1.Service) {
			s.Spec.Template.Spec.Containers[0].TTY = true
		},
		assertIfNoError: func(t *testing.T, svc *v1.Service) {
			if svc.Spec.Template.Spec.Containers[0].TTY == true {
				t.Error("Expected TTY to be pruned")
			}
		},
	}, {
		name: "TestInvalidUID",
		options: func(s *v1.Service) {
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

			svc, err := testv1.CreateService(t, clients, names, tc.options)
			if err == nil && tc.assertIfNoError == nil {
				t.Errorf("CreateService = %v, want: error", spew.Sdump(svc))
			} else if err == nil && tc.assertIfNoError != nil {
				tc.assertIfNoError(t, svc)
			}
		})
	}
}
