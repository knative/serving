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

package serving

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"knative.dev/pkg/kmp"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/config"
)

func TestVolumeMask(t *testing.T) {
	want := &corev1.Volume{
		Name:         "foo",
		VolumeSource: corev1.VolumeSource{},
	}
	in := want

	got := VolumeMask(context.Background(), in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("VolumeMask (-want, +got):", diff)
	}

	if got = VolumeMask(context.Background(), nil); got != nil {
		t.Errorf("VolumeMask(nil) = %v, want: nil", got)
	}
}

func TestCapabilitiesMask_SecurePodDefaultsEnabled(t *testing.T) {
	// Ensures users can only add NET_BIND_SERVICE or nil capabilities
	tests := []struct {
		name string
		in   *corev1.Capabilities
		want *corev1.Capabilities
	}{{
		name: "empty",
		in: &corev1.Capabilities{
			Add: nil,
		},
		want: &corev1.Capabilities{
			Add: nil,
		},
	}, {
		name: "allows NET_BIND_SERVICE capability",
		in: &corev1.Capabilities{
			Add: []corev1.Capability{"NET_BIND_SERVICE"},
		},
		want: &corev1.Capabilities{
			Add: []corev1.Capability{"NET_BIND_SERVICE"},
		},
	}, {
		name: "prevents restricted fields",
		in: &corev1.Capabilities{
			Add: []corev1.Capability{"CHOWN"},
		},
		want: &corev1.Capabilities{
			Add: nil,
		},
	}}

	for _, test := range tests {
		ctx := config.ToContext(context.Background(),
			&config.Config{
				Features: &config.Features{
					SecurePodDefaults: config.Enabled,
				},
			},
		)

		t.Run(test.name, func(t *testing.T) {
			got := CapabilitiesMask(ctx, test.in)

			if &test.want == &got {
				t.Error("Input and output share addresses. Want different addresses")
			}

			if diff, err := kmp.SafeDiff(test.want, got); err != nil {
				t.Error("Got error comparing output, err =", err)
			} else if diff != "" {
				t.Error("CapabilitiesMask (-want, +got):", diff)
			}

			if got = CapabilitiesMask(ctx, nil); got != nil {
				t.Errorf("CapabilitiesMask = %v, want: nil", got)
			}
		})
	}

}

func TestVolumeSourceMask(t *testing.T) {
	want := &corev1.VolumeSource{
		Secret:    &corev1.SecretVolumeSource{},
		ConfigMap: &corev1.ConfigMapVolumeSource{},
	}
	in := &corev1.VolumeSource{
		Secret:    &corev1.SecretVolumeSource{},
		ConfigMap: &corev1.ConfigMapVolumeSource{},
		NFS:       &corev1.NFSVolumeSource{},
	}

	got := VolumeSourceMask(context.Background(), in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("VolumeSourceMask (-want, +got):", diff)
	}

	if got = VolumeSourceMask(context.Background(), nil); got != nil {
		t.Errorf("VolumeSourceMask(nil) = %v, want: nil", got)
	}
}

func TestVolumeProjectionMask(t *testing.T) {
	want := &corev1.VolumeProjection{
		DownwardAPI: &corev1.DownwardAPIProjection{
			Items: []corev1.DownwardAPIVolumeFile{
				{
					Path: "labels",
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.labels",
					},
				},
				{
					Path: "cpu_limit",
					ResourceFieldRef: &corev1.ResourceFieldSelector{
						ContainerName: "bar",
						Resource:      "limits.cpu",
					},
				},
			},
		},
	}

	in := &corev1.VolumeProjection{
		DownwardAPI: &corev1.DownwardAPIProjection{
			Items: []corev1.DownwardAPIVolumeFile{
				{
					Path: "labels",
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.labels",
					},
				},
				{
					Path: "cpu_limit",
					ResourceFieldRef: &corev1.ResourceFieldSelector{
						ContainerName: "bar",
						Resource:      "limits.cpu",
					},
				},
			},
		},
	}

	got := VolumeProjectionMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("VolumeProjectionMask (-want, +got):", diff)
	}

	if got = VolumeProjectionMask(nil); got != nil {
		t.Errorf("VolumeProjectionMask(nil) = %v, want: nil", got)
	}
}

func TestPodSpecMask(t *testing.T) {
	want := &corev1.PodSpec{
		ServiceAccountName: "default",
		ImagePullSecrets: []corev1.LocalObjectReference{{
			Name: "foo",
		}},
		Containers: []corev1.Container{{
			Image: "helloworld",
		}},
		Volumes: []corev1.Volume{{
			Name: "the-name",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "foo",
				},
			},
		}},
	}
	in := &corev1.PodSpec{
		ServiceAccountName: "default",
		ImagePullSecrets: []corev1.LocalObjectReference{{
			Name: "foo",
		}},
		Containers: []corev1.Container{{
			Image: "helloworld",
		}},
		Volumes: []corev1.Volume{{
			Name: "the-name",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "foo",
				},
			},
		}},
		// Stripped out.
		InitContainers: []corev1.Container{{
			Image: "busybox",
		}},
	}

	ctx := context.Background()
	got := PodSpecMask(ctx, in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("PodSpecMask (-want, +got):", diff)
	}

	if got = PodSpecMask(ctx, nil); got != nil {
		t.Errorf("PodSpecMask(nil) = %v, want: nil", got)
	}
}

func TestContainerMask(t *testing.T) {
	want := &corev1.Container{
		Name:                     "foo",
		Args:                     []string{"hello"},
		Command:                  []string{"world"},
		Env:                      []corev1.EnvVar{{}},
		EnvFrom:                  []corev1.EnvFromSource{{}},
		Image:                    "python",
		ImagePullPolicy:          corev1.PullAlways,
		LivenessProbe:            &corev1.Probe{},
		Ports:                    []corev1.ContainerPort{{}},
		ReadinessProbe:           &corev1.Probe{},
		Resources:                corev1.ResourceRequirements{},
		SecurityContext:          &corev1.SecurityContext{},
		StartupProbe:             &corev1.Probe{},
		TerminationMessagePath:   "/",
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		VolumeMounts:             []corev1.VolumeMount{{}},
	}
	in := &corev1.Container{
		Args:                     []string{"hello"},
		Command:                  []string{"world"},
		Env:                      []corev1.EnvVar{{}},
		EnvFrom:                  []corev1.EnvFromSource{{}},
		Image:                    "python",
		ImagePullPolicy:          corev1.PullAlways,
		LivenessProbe:            &corev1.Probe{},
		Ports:                    []corev1.ContainerPort{{}},
		ReadinessProbe:           &corev1.Probe{},
		Resources:                corev1.ResourceRequirements{},
		SecurityContext:          &corev1.SecurityContext{},
		StartupProbe:             &corev1.Probe{},
		TerminationMessagePath:   "/",
		TerminationMessagePolicy: corev1.TerminationMessageReadFile,
		VolumeMounts:             []corev1.VolumeMount{{}},
		Name:                     "foo",
		Stdin:                    true,
		StdinOnce:                true,
		TTY:                      true,
	}

	got := ContainerMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("ContainerMask (-want, +got):", diff)
	}

	if got = ContainerMask(nil); got != nil {
		t.Errorf("ContainerMask(nil) = %v, want: nil", got)
	}
}

func TestVolumeMountMask(t *testing.T) {
	mode := corev1.MountPropagationBidirectional

	want := &corev1.VolumeMount{
		Name:      "foo",
		ReadOnly:  true,
		MountPath: "/foo/bar",
		SubPath:   "baz",
	}
	in := &corev1.VolumeMount{
		Name:             "foo",
		ReadOnly:         true,
		MountPath:        "/foo/bar",
		SubPath:          "baz",
		MountPropagation: &mode,
	}

	got := VolumeMountMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("VolumeMountMask (-want, +got):", diff)
	}

	if got = VolumeMountMask(nil); got != nil {
		t.Errorf("VolumeMountMask(nil) = %v, want: nil", got)
	}
}

func TestProbeMask(t *testing.T) {
	want := &corev1.Probe{
		ProbeHandler:        corev1.ProbeHandler{},
		InitialDelaySeconds: 42,
		TimeoutSeconds:      42,
		PeriodSeconds:       42,
		SuccessThreshold:    42,
		FailureThreshold:    42,
	}
	in := want

	got := ProbeMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("ProbeMask (-want, +got):", diff)
	}

	if got = ProbeMask(nil); got != nil {
		t.Errorf("ProbeMask(nil) = %v, want: nil", got)
	}
}

func TestHandlerMask(t *testing.T) {
	want := &corev1.ProbeHandler{
		Exec:      &corev1.ExecAction{},
		HTTPGet:   &corev1.HTTPGetAction{},
		TCPSocket: &corev1.TCPSocketAction{},
		GRPC:      &corev1.GRPCAction{},
	}
	in := want

	got := HandlerMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("HandlerMask (-want, +got):", diff)
	}

	if got = HandlerMask(nil); got != nil {
		t.Errorf("HandlerMask(nil) = %v, want: nil", got)
	}
}

func TestExecActionMask(t *testing.T) {
	want := &corev1.ExecAction{
		Command: []string{"foo", "bar"},
	}
	in := want

	got := ExecActionMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("ExecActionMask (-want, +got):", diff)
	}

	if got = ExecActionMask(nil); got != nil {
		t.Errorf("ExecActionMask(nil) = %v, want: nil", got)
	}
}

func TestHTTPGetActionMask(t *testing.T) {
	want := &corev1.HTTPGetAction{
		Host:        "foo",
		Path:        "/bar",
		Scheme:      corev1.URISchemeHTTP,
		HTTPHeaders: []corev1.HTTPHeader{{}},
		Port:        intstr.FromInt(8080),
	}
	in := &corev1.HTTPGetAction{
		Host:        "foo",
		Path:        "/bar",
		Scheme:      corev1.URISchemeHTTP,
		HTTPHeaders: []corev1.HTTPHeader{{}},
		Port:        intstr.FromInt(8080),
	}

	got := HTTPGetActionMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("HTTPGetActionMask (-want, +got):", diff)
	}

	if got = HTTPGetActionMask(nil); got != nil {
		t.Errorf("HTTPGetActionMask(nil) = %v, want: nil", got)
	}
}

func TestTCPSocketActionMask(t *testing.T) {
	want := &corev1.TCPSocketAction{
		Host: "foo",
		Port: intstr.FromString("https"),
	}
	in := &corev1.TCPSocketAction{
		Host: "foo",
		Port: intstr.FromString("https"),
	}

	got := TCPSocketActionMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("TCPSocketActionMask (-want, +got):", diff)
	}

	if got = TCPSocketActionMask(nil); got != nil {
		t.Errorf("TCPSocketActionMask(nil) = %v, want: nil", got)
	}
}

func TestGRPCActionMask(t *testing.T) {
	want := &corev1.GRPCAction{
		Port:    42,
		Service: ptr.String("foo"),
	}
	in := &corev1.GRPCAction{
		Port:    42,
		Service: ptr.String("foo"),
	}

	got := GRPCActionMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("GRPCActionMask (-want, +got):", diff)
	}

	if got = GRPCActionMask(nil); got != nil {
		t.Errorf("GRPCActionMask(nil) = %v, want: nil", got)
	}
}

func TestContainerPortMask(t *testing.T) {
	want := &corev1.ContainerPort{
		ContainerPort: 42,
		Name:          "foo",
		Protocol:      corev1.ProtocolTCP,
	}
	in := &corev1.ContainerPort{
		ContainerPort: 42,
		Name:          "foo",
		Protocol:      corev1.ProtocolTCP,
		HostIP:        "10.0.0.1",
		HostPort:      43,
	}

	got := ContainerPortMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("ContainerPortMask (-want, +got):", diff)
	}

	if got = ContainerPortMask(nil); got != nil {
		t.Errorf("ContainerPortMask(nil) = %v, want: nil", got)
	}
}

func TestEnvVarMask(t *testing.T) {
	want := &corev1.EnvVar{
		Name:      "foo",
		Value:     "bar",
		ValueFrom: &corev1.EnvVarSource{},
	}
	in := want

	got := EnvVarMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("EnvVarMask (-want, +got):", diff)
	}

	if got = EnvVarMask(nil); got != nil {
		t.Errorf("EnvVarMask(nil) = %v, want: nil", got)
	}
}

func TestEnvVarSourceMask(t *testing.T) {
	tests := []struct {
		name     string
		fieldRef bool
		in       *corev1.EnvVarSource
		want     *corev1.EnvVarSource
	}{{
		name:     "FieldRef false",
		fieldRef: false,
		in: &corev1.EnvVarSource{
			ConfigMapKeyRef:  &corev1.ConfigMapKeySelector{},
			SecretKeyRef:     &corev1.SecretKeySelector{},
			FieldRef:         &corev1.ObjectFieldSelector{},
			ResourceFieldRef: &corev1.ResourceFieldSelector{},
		},
		want: &corev1.EnvVarSource{
			ConfigMapKeyRef: &corev1.ConfigMapKeySelector{},
			SecretKeyRef:    &corev1.SecretKeySelector{},
		},
	}, {
		name:     "FieldRef true",
		fieldRef: true,
		in: &corev1.EnvVarSource{
			ConfigMapKeyRef:  &corev1.ConfigMapKeySelector{},
			SecretKeyRef:     &corev1.SecretKeySelector{},
			FieldRef:         &corev1.ObjectFieldSelector{},
			ResourceFieldRef: &corev1.ResourceFieldSelector{},
		},
		want: &corev1.EnvVarSource{
			ConfigMapKeyRef:  &corev1.ConfigMapKeySelector{},
			SecretKeyRef:     &corev1.SecretKeySelector{},
			FieldRef:         &corev1.ObjectFieldSelector{},
			ResourceFieldRef: &corev1.ResourceFieldSelector{},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := EnvVarSourceMask(test.in, test.fieldRef)

			if &test.want == &got {
				t.Error("Input and output share addresses. Want different addresses")
			}

			if diff, err := kmp.SafeDiff(test.want, got); err != nil {
				t.Error("Got error comparing output, err =", err)
			} else if diff != "" {
				t.Error("EnvVarSourceMask (-want, +got):", diff)
			}

			if got = EnvVarSourceMask(nil, test.fieldRef); got != nil {
				t.Errorf("EnvVarSourceMask(nil) = %v, want: nil", got)
			}
		})
	}
}

func TestLocalObjectReferenceMask(t *testing.T) {
	want := &corev1.LocalObjectReference{
		Name: "foo",
	}
	in := want

	got := LocalObjectReferenceMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("LocalObjectReferenceMask (-want, +got):", diff)
	}

	if got = LocalObjectReferenceMask(nil); got != nil {
		t.Errorf("LocalObjectReferenceMask(nil) = %v, want: nil", got)
	}
}

func TestConfigMapKeySelectorMask(t *testing.T) {
	want := &corev1.ConfigMapKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{},
		Key:                  "foo",
		Optional:             ptr.Bool(true),
	}
	in := want

	got := ConfigMapKeySelectorMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("ConfigMapKeySelectorMask (-want, +got):", diff)
	}

	if got = ConfigMapKeySelectorMask(nil); got != nil {
		t.Errorf("ConfigMapKeySelectorMask(nil) = %v, want: nil", got)
	}
}

func TestSecretKeySelectorMask(t *testing.T) {
	want := &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{},
		Key:                  "foo",
		Optional:             ptr.Bool(true),
	}
	in := want

	got := SecretKeySelectorMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("SecretKeySelectorMask (-want, +got):", diff)
	}

	if got = SecretKeySelectorMask(nil); got != nil {
		t.Errorf("SecretKeySelectorMask(nil) = %v, want: nil", got)
	}
}

func TestConfigMapEnvSourceMask(t *testing.T) {
	want := &corev1.ConfigMapEnvSource{
		LocalObjectReference: corev1.LocalObjectReference{},
		Optional:             ptr.Bool(true),
	}
	in := want

	got := ConfigMapEnvSourceMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("ConfigMapEnvSourceMask (-want, +got):", diff)
	}

	if got = ConfigMapEnvSourceMask(nil); got != nil {
		t.Errorf("ConfigMapEnvSourceMask(nil) = %v, want: nil", got)
	}
}

func TestSecretEnvSourceMask(t *testing.T) {
	want := &corev1.SecretEnvSource{
		LocalObjectReference: corev1.LocalObjectReference{},
		Optional:             ptr.Bool(true),
	}
	in := want

	got := SecretEnvSourceMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("SecretEnvSourceMask (-want, +got):", diff)
	}

	if got = SecretEnvSourceMask(nil); got != nil {
		t.Errorf("SecretEnvSourceMask(nil) = %v, want: nil", got)
	}
}

func TestEnvFromSourceMask(t *testing.T) {
	want := &corev1.EnvFromSource{
		Prefix:       "foo",
		ConfigMapRef: &corev1.ConfigMapEnvSource{},
		SecretRef:    &corev1.SecretEnvSource{},
	}
	in := want

	got := EnvFromSourceMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("EnvFromSourceMask (-want, +got):", diff)
	}

	if got = EnvFromSourceMask(nil); got != nil {
		t.Errorf("EnvFromSourceMask(nil) = %v, want: nil", got)
	}
}

func TestResourceRequirementsMask(t *testing.T) {
	want := &corev1.ResourceRequirements{
		Limits:   make(corev1.ResourceList),
		Requests: make(corev1.ResourceList),
	}
	in := want

	got := ResourceRequirementsMask(in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("ResourceRequirementsMask (-want, +got):", diff)
	}

	if got = ResourceRequirementsMask(nil); got != nil {
		t.Errorf("ResourceRequirementsMask(nil) = %v, want: nil", got)
	}
}

func TestPodSecurityContextMask(t *testing.T) {
	in := &corev1.PodSecurityContext{
		SELinuxOptions:     &corev1.SELinuxOptions{},
		WindowsOptions:     &corev1.WindowsSecurityContextOptions{},
		SupplementalGroups: []int64{},
		Sysctls:            []corev1.Sysctl{},
		RunAsUser:          ptr.Int64(1),
		RunAsGroup:         ptr.Int64(1),
		RunAsNonRoot:       ptr.Bool(true),
		FSGroup:            ptr.Int64(1),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}

	want := &corev1.PodSecurityContext{}
	ctx := context.Background()

	got := PodSecurityContextMask(ctx, in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("PostSecurityContextMask (-want, +got):", diff)
	}

	if got = PodSecurityContextMask(ctx, nil); got != nil {
		t.Errorf("PodSecurityContextMask(nil) = %v, want: nil", got)
	}
}

func TestPodSecurityContextMask_FeatureEnabled(t *testing.T) {
	in := &corev1.PodSecurityContext{
		SELinuxOptions:     &corev1.SELinuxOptions{},
		WindowsOptions:     &corev1.WindowsSecurityContextOptions{},
		SupplementalGroups: []int64{1},
		Sysctls:            []corev1.Sysctl{},
		RunAsUser:          ptr.Int64(1),
		RunAsGroup:         ptr.Int64(1),
		RunAsNonRoot:       ptr.Bool(true),
		FSGroup:            ptr.Int64(1),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}

	want := &corev1.PodSecurityContext{
		RunAsUser:          ptr.Int64(1),
		RunAsGroup:         ptr.Int64(1),
		RunAsNonRoot:       ptr.Bool(true),
		FSGroup:            ptr.Int64(1),
		SupplementalGroups: []int64{1},
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}

	ctx := config.ToContext(context.Background(),
		&config.Config{
			Features: &config.Features{
				PodSpecSecurityContext: config.Enabled,
			},
		},
	)

	got := PodSecurityContextMask(ctx, in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("PostSecurityContextMask (-want, +got):", diff)
	}
}

func TestPodSecurityContextMask_SecurePodDefaultsEnabled(t *testing.T) {

	// Ensure that users can opt out of better security by explicitly
	// requesting the Kubernetes default, which is "Unconfined".
	want := &corev1.PodSecurityContext{
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeUnconfined,
		},
	}

	in := &corev1.PodSecurityContext{
		SELinuxOptions:     &corev1.SELinuxOptions{},
		WindowsOptions:     &corev1.WindowsSecurityContextOptions{},
		SupplementalGroups: []int64{1},
		Sysctls:            []corev1.Sysctl{},
		RunAsUser:          ptr.Int64(1),
		RunAsGroup:         ptr.Int64(1),
		RunAsNonRoot:       ptr.Bool(true),
		FSGroup:            ptr.Int64(1),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeUnconfined,
		},
	}

	ctx := config.ToContext(context.Background(),
		&config.Config{
			Features: &config.Features{
				SecurePodDefaults:      config.Enabled,
				PodSpecSecurityContext: config.Disabled,
			},
		},
	)

	got := PodSecurityContextMask(ctx, in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("PostSecurityContextMask (-want, +got):", diff)
	}
}

func TestSecurityContextMask(t *testing.T) {
	mtype := corev1.UnmaskedProcMount
	want := &corev1.SecurityContext{
		Capabilities:             &corev1.Capabilities{},
		RunAsUser:                ptr.Int64(1),
		RunAsGroup:               ptr.Int64(2),
		RunAsNonRoot:             ptr.Bool(true),
		ReadOnlyRootFilesystem:   ptr.Bool(true),
		AllowPrivilegeEscalation: ptr.Bool(false),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
	in := &corev1.SecurityContext{
		RunAsUser:                ptr.Int64(1),
		Capabilities:             &corev1.Capabilities{},
		Privileged:               ptr.Bool(true),
		SELinuxOptions:           &corev1.SELinuxOptions{},
		RunAsGroup:               ptr.Int64(2),
		RunAsNonRoot:             ptr.Bool(true),
		ReadOnlyRootFilesystem:   ptr.Bool(true),
		AllowPrivilegeEscalation: ptr.Bool(false),
		ProcMount:                &mtype,
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
		WindowsOptions: &corev1.WindowsSecurityContextOptions{},
	}

	got := SecurityContextMask(context.Background(), in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("SecurityContextMask (-want, +got):", diff)
	}

	if got = SecurityContextMask(context.Background(), nil); got != nil {
		t.Errorf("SecurityContextMask(nil) = %v, want: nil", got)
	}
}

func TestSecurityContextMask_FeatureEnabled(t *testing.T) {
	mtype := corev1.UnmaskedProcMount
	want := &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.Bool(false),
		Capabilities:             &corev1.Capabilities{},
		RunAsGroup:               ptr.Int64(2),
		RunAsNonRoot:             ptr.Bool(true),
		RunAsUser:                ptr.Int64(1),
		ReadOnlyRootFilesystem:   ptr.Bool(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
	in := &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.Bool(false),
		Capabilities:             &corev1.Capabilities{},
		Privileged:               ptr.Bool(true),
		ProcMount:                &mtype,
		ReadOnlyRootFilesystem:   ptr.Bool(true),
		RunAsGroup:               ptr.Int64(2),
		RunAsNonRoot:             ptr.Bool(true),
		RunAsUser:                ptr.Int64(1),
		SELinuxOptions:           &corev1.SELinuxOptions{},
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
		WindowsOptions: &corev1.WindowsSecurityContextOptions{},
	}

	ctx := config.ToContext(context.Background(),
		&config.Config{
			Features: &config.Features{
				PodSpecSecurityContext: config.Enabled,
			},
		},
	)

	got := SecurityContextMask(ctx, in)

	if &want == &got {
		t.Error("Input and output share addresses. Want different addresses")
	}

	if diff, err := kmp.SafeDiff(want, got); err != nil {
		t.Error("Got error comparing output, err =", err)
	} else if diff != "" {
		t.Error("SecurityContextMask (-want, +got):", diff)
	}
}
