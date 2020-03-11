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

package v1

import (
	"context"

	"github.com/google/uuid"

	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/config"
)

// SetDefaults implements apis.Defaultable
func (r *Revision) SetDefaults(ctx context.Context) {
	r.Spec.SetDefaults(apis.WithinSpec(ctx))
}

// SetDefaults implements apis.Defaultable
func (rts *RevisionTemplateSpec) SetDefaults(ctx context.Context) {
	rts.Spec.SetDefaults(apis.WithinSpec(ctx))
}

// SetDefaults implements apis.Defaultable
func (rs *RevisionSpec) SetDefaults(ctx context.Context) {
	cfg := config.FromContextOrDefaults(ctx)

	// Default TimeoutSeconds based on our configmap
	if rs.TimeoutSeconds == nil || *rs.TimeoutSeconds == 0 {
		rs.TimeoutSeconds = ptr.Int64(cfg.Defaults.RevisionTimeoutSeconds)
	}

	// Default ContainerConcurrency based on our configmap
	if rs.ContainerConcurrency == nil {
		rs.ContainerConcurrency = ptr.Int64(cfg.Defaults.ContainerConcurrency)
	}

	for idx := range rs.PodSpec.Containers {
		if rs.PodSpec.Containers[idx].Name == "" {
			if len(rs.PodSpec.Containers) > 1 {
				rs.PodSpec.Containers[idx].Name = kmeta.ChildName(cfg.Defaults.UserContainerName(ctx), "-"+uuid.New().String())
			} else {
				rs.PodSpec.Containers[idx].Name = cfg.Defaults.UserContainerName(ctx)
			}
		}

		rs.applyDefault(&rs.PodSpec.Containers[idx], cfg)
	}
}

func (rs *RevisionSpec) applyDefault(container *corev1.Container, cfg *config.Config) {
	if container.Resources.Requests == nil {
		container.Resources.Requests = corev1.ResourceList{}
	}
	if _, ok := container.Resources.Requests[corev1.ResourceCPU]; !ok {
		if rc := cfg.Defaults.RevisionCPURequest; rc != nil {
			container.Resources.Requests[corev1.ResourceCPU] = *rc
		}
	}
	if _, ok := container.Resources.Requests[corev1.ResourceMemory]; !ok {
		if rm := cfg.Defaults.RevisionMemoryRequest; rm != nil {
			container.Resources.Requests[corev1.ResourceMemory] = *rm
		}
	}

	if container.Resources.Limits == nil {
		container.Resources.Limits = corev1.ResourceList{}
	}
	if _, ok := container.Resources.Limits[corev1.ResourceCPU]; !ok {
		if rc := cfg.Defaults.RevisionCPULimit; rc != nil {
			container.Resources.Limits[corev1.ResourceCPU] = *rc
		}
	}
	if _, ok := container.Resources.Limits[corev1.ResourceMemory]; !ok {
		if rm := cfg.Defaults.RevisionMemoryLimit; rm != nil {
			container.Resources.Limits[corev1.ResourceMemory] = *rm
		}
	}

	// If there are multiple containers then default probes will be applied to the container where user specified PORT
	// default probes will not be applied for non serving containers
	if len(rs.PodSpec.Containers) == 1 || len(container.Ports) != 0 {
		rs.applyProbes(container)
	}

	vms := container.VolumeMounts
	for i := range vms {
		vms[i].ReadOnly = true
	}
}

func (*RevisionSpec) applyProbes(container *corev1.Container) {
	if container.ReadinessProbe == nil {
		container.ReadinessProbe = &corev1.Probe{}
	}
	if container.ReadinessProbe.TCPSocket == nil &&
		container.ReadinessProbe.HTTPGet == nil &&
		container.ReadinessProbe.Exec == nil {
		container.ReadinessProbe.TCPSocket = &corev1.TCPSocketAction{}
	}

	if container.ReadinessProbe.SuccessThreshold == 0 {
		container.ReadinessProbe.SuccessThreshold = 1
	}

}
