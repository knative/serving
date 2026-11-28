/*
Copyright 2026 The Knative Authors

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

package serving

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/pkg/apis"
	"knative.dev/serving/pkg/apis/config"
)

func TestNativeSidecarMask(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, init := range []bool{false, true} {
			ctx := t.Context()
			cfg := config.FromContextOrDefaults(ctx)
			if enabled {
				cfg.Features.PodSpecInitContainers = config.Enabled
			}
			ctx = config.ToContext(ctx, cfg)
			if init {
				ctx = WithinInitContainer(ctx)
			}
			in := &corev1.Container{Image: "example.com/helper", RestartPolicy: new(corev1.ContainerRestartPolicyAlways)}
			before := in.DeepCopy()
			got := ContainerMask(ctx, in)
			if (got.RestartPolicy != nil) != (enabled && init) {
				t.Errorf("enabled=%v init=%v: restart policy = %v", enabled, init, got.RestartPolicy)
			}
			if diff := cmp.Diff(before, in); diff != "" {
				t.Errorf("mask mutated input (-want +got): %s", diff)
			}
		}
	}
}

func TestNativeSidecarValidation(t *testing.T) {
	httpProbe := &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt(9000)}}}
	for _, tc := range []struct {
		name     string
		policy   *corev1.ContainerRestartPolicy
		probe    *corev1.Probe
		disabled bool
		invalid  bool
	}{
		{name: "ordinary"},
		{name: "ordinary probe", probe: httpProbe, invalid: true},
		{name: "restartable", policy: new(corev1.ContainerRestartPolicyAlways)},
		{name: "http", policy: new(corev1.ContainerRestartPolicyAlways), probe: httpProbe},
		{name: "tcp", policy: new(corev1.ContainerRestartPolicyAlways), probe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(9000)}}}},
		{name: "exec", policy: new(corev1.ContainerRestartPolicyAlways), probe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"true"}}}}},
		{name: "grpc", policy: new(corev1.ContainerRestartPolicyAlways), probe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{GRPC: &corev1.GRPCAction{Port: 9000}}}},
		{name: "missing handler", policy: new(corev1.ContainerRestartPolicyAlways), probe: &corev1.Probe{}, invalid: true},
		{name: "missing http port", policy: new(corev1.ContainerRestartPolicyAlways), probe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/health"}}}, invalid: true},
		{name: "missing tcp port", policy: new(corev1.ContainerRestartPolicyAlways), probe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{}}}, invalid: true},
		{name: "multiple handlers", policy: new(corev1.ContainerRestartPolicyAlways), probe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromInt(9000)}, Exec: &corev1.ExecAction{Command: []string{"true"}},
		}}, invalid: true},
		{name: "empty policy", policy: new(corev1.ContainerRestartPolicy("")), invalid: true},
		{name: "never", policy: new(corev1.ContainerRestartPolicy("Never")), invalid: true},
		{name: "on failure", policy: new(corev1.ContainerRestartPolicy("OnFailure")), invalid: true},
		{name: "unknown policy", policy: new(corev1.ContainerRestartPolicy("unknown")), invalid: true},
		{name: "disabled", policy: new(corev1.ContainerRestartPolicyAlways), disabled: true, invalid: true},
		{name: "disabled probe", policy: new(corev1.ContainerRestartPolicyAlways), probe: httpProbe, disabled: true, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			cfg := config.FromContextOrDefaults(ctx)
			if !tc.disabled {
				cfg.Features.PodSpecInitContainers = config.Enabled
			}
			ctx = config.ToContext(ctx, cfg)
			container := corev1.Container{Image: "example.com/helper", RestartPolicy: tc.policy, StartupProbe: tc.probe}
			err := validateInitContainer(ctx, container, nil).Filter(apis.ErrorLevel)
			if (err != nil) != tc.invalid {
				t.Errorf("validateInitContainer() = %v, want invalid=%v", err, tc.invalid)
			}
			pod := corev1.PodSpec{
				Containers:     []corev1.Container{{Name: "application", Image: "example.com/application"}},
				InitContainers: []corev1.Container{container},
			}
			if err := ValidatePodSpec(ctx, pod).Filter(apis.ErrorLevel); (err != nil) != tc.invalid {
				t.Errorf("ValidatePodSpec() = %v, want invalid=%v", err, tc.invalid)
			}
			if tc.policy != nil {
				if err := validate(ctx, container, nil).Filter(apis.ErrorLevel); err == nil {
					t.Error("regular container accepted restartPolicy")
				}
			}
			for _, field := range []string{"liveness", "readiness", "lifecycle"} {
				c := container.DeepCopy()
				switch field {
				case "liveness":
					c.LivenessProbe = httpProbe
				case "readiness":
					c.ReadinessProbe = httpProbe
				case "lifecycle":
					c.Lifecycle = &corev1.Lifecycle{}
				}
				if err := validateInitContainer(ctx, *c, nil).Filter(apis.ErrorLevel); err == nil {
					t.Errorf("init container accepted %s", field)
				}
			}
		})
	}
}
