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

package v1beta1

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	"knative.dev/serving/pkg/apis/config"
)

// SetDefaults implements apis.Defaultable
func (r *Revision) SetDefaults(ctx context.Context) {
	r.Spec.SetDefaults(ctx)
}

// SetDefaults implements apis.Defaultable
func (rts *RevisionTemplateSpec) SetDefaults(ctx context.Context) {
	rts.Spec.SetDefaults(ctx)
}

// SetDefaults implements apis.Defaultable
func (rs *RevisionSpec) SetDefaults(ctx context.Context) {
	cfg := config.FromContextOrDefaults(ctx)

	// Default TimeoutSeconds based on our configmap
	if rs.TimeoutSeconds == nil {
		ts := cfg.Defaults.RevisionTimeoutSeconds
		rs.TimeoutSeconds = &ts
	}

	for idx := range rs.PodSpec.Containers {
		ctr := &rs.PodSpec.Containers[idx]

		if ctr.Name == "" {
			ctr.Name = cfg.Defaults.UserContainerName(ctx)
		}

		if ctr.Resources.Requests == nil {
			ctr.Resources.Requests = corev1.ResourceList{}
		}
		if _, ok := ctr.Resources.Requests[corev1.ResourceCPU]; !ok {
			if rsrc := cfg.Defaults.RevisionCPURequest; rsrc != nil {
				ctr.Resources.Requests[corev1.ResourceCPU] = *rsrc
			}
		}
		if _, ok := ctr.Resources.Requests[corev1.ResourceMemory]; !ok {
			if rsrc := cfg.Defaults.RevisionMemoryRequest; rsrc != nil {
				ctr.Resources.Requests[corev1.ResourceMemory] = *rsrc
			}
		}

		if ctr.Resources.Limits == nil {
			ctr.Resources.Limits = corev1.ResourceList{}
		}
		if _, ok := ctr.Resources.Limits[corev1.ResourceCPU]; !ok {
			if rsrc := cfg.Defaults.RevisionCPULimit; rsrc != nil {
				ctr.Resources.Limits[corev1.ResourceCPU] = *rsrc
			}
		}
		if _, ok := ctr.Resources.Limits[corev1.ResourceMemory]; !ok {
			if rsrc := cfg.Defaults.RevisionMemoryLimit; rsrc != nil {
				ctr.Resources.Limits[corev1.ResourceMemory] = *rsrc
			}
		}

		if ctr.ReadinessProbe == nil {
			ctr.ReadinessProbe = &corev1.Probe{}
		}
		if ctr.ReadinessProbe.TCPSocket == nil &&
			ctr.ReadinessProbe.HTTPGet == nil &&
			ctr.ReadinessProbe.Exec == nil {
			ctr.ReadinessProbe.TCPSocket = &corev1.TCPSocketAction{}
		}
		if ctr.ReadinessProbe.SuccessThreshold == 0 {
			ctr.ReadinessProbe.SuccessThreshold = 1
		}
		// If any of FailureThreshold, TimeoutSeconds or PeriodSeconds are greater than 0,
		// standard k8s-style would be used so setting normal k8s default values.
		if ctr.ReadinessProbe.FailureThreshold > 0 ||
			ctr.ReadinessProbe.TimeoutSeconds > 0 ||
			ctr.ReadinessProbe.PeriodSeconds > 0 {
			if ctr.ReadinessProbe.FailureThreshold == 0 {
				ctr.ReadinessProbe.FailureThreshold = 3
			}
			if ctr.ReadinessProbe.TimeoutSeconds == 0 {
				ctr.ReadinessProbe.TimeoutSeconds = 1
			}
			if ctr.ReadinessProbe.PeriodSeconds == 0 {
				ctr.ReadinessProbe.PeriodSeconds = 10
			}
		}

		vms := ctr.VolumeMounts
		for i := range vms {
			vms[i].ReadOnly = true
		}
	}
}
