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
	"k8s.io/apimachinery/pkg/api/resource"
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

	if container.Resources.Limits == nil {
		container.Resources.Limits = corev1.ResourceList{}
	}

	for _, r := range []struct {
		Name    corev1.ResourceName
		Request *resource.Quantity
		Limit   *resource.Quantity
	}{{
		Name:    corev1.ResourceCPU,
		Request: cfg.Defaults.RevisionCPURequest,
		Limit:   cfg.Defaults.RevisionCPULimit,
	}, {
		Name:    corev1.ResourceMemory,
		Request: cfg.Defaults.RevisionMemoryRequest,
		Limit:   cfg.Defaults.RevisionMemoryLimit,
	}, {
		Name:    corev1.ResourceEphemeralStorage,
		Request: cfg.Defaults.RevisionEphemeralStorageRequest,
		Limit:   cfg.Defaults.RevisionEphemeralStorageLimit,
	}} {
		if _, ok := container.Resources.Requests[r.Name]; !ok && r.Request != nil {
			container.Resources.Requests[r.Name] = *r.Request
		}
		if _, ok := container.Resources.Limits[r.Name]; !ok && r.Limit != nil {
			container.Resources.Limits[r.Name] = *r.Limit
		}
	}

	// If there are multiple containers then default probes will be applied to the container where user specified PORT
	// default probes will not be applied for non serving containers
	if len(rs.PodSpec.Containers) == 1 || len(container.Ports) != 0 {
		rs.applyProbes(container)
	}

	if rs.PodSpec.EnableServiceLinks == nil {
		rs.PodSpec.EnableServiceLinks = cfg.Defaults.EnableServiceLinks
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
