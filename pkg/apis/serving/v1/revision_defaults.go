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
	"cmp"
	"context"
	"slices"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/ptr"
	"knative.dev/serving/pkg/apis/config"
)

// SetDefaults implements apis.Defaultable
func (r *Revision) SetDefaults(ctx context.Context) {
	// SetDefaults may update revision spec which is immutable.
	// See: https://github.com/knative/serving/issues/8128 for details.
	if apis.IsInUpdate(ctx) {
		return
	}
	r.Spec.SetDefaults(apis.WithinSpec(ctx))
}

// SetDefaults implements apis.Defaultable
func (rts *RevisionTemplateSpec) SetDefaults(ctx context.Context) {
	rts.Spec.SetDefaults(apis.WithinSpec(ctx))
}

// SetDefaults implements apis.Defaultable
func (rs *RevisionSpec) SetDefaults(ctx context.Context) {
	cfg := config.FromContextOrDefaults(ctx)

	// Default TimeoutSeconds based on our configmap.
	if rs.TimeoutSeconds == nil || *rs.TimeoutSeconds == 0 {
		rs.TimeoutSeconds = ptr.Int64(cfg.Defaults.RevisionTimeoutSeconds)
	}

	// Default IdleTimeoutSeconds only in case we have a non-zero default and the latter is not larger than the revision timeout.
	// A zero default or a zero value set from the user or a nil value skips timer setup at the QP side.
	if rs.IdleTimeoutSeconds == nil {
		if cfg.Defaults.RevisionIdleTimeoutSeconds < *rs.TimeoutSeconds && cfg.Defaults.RevisionIdleTimeoutSeconds != 0 {
			rs.IdleTimeoutSeconds = ptr.Int64(cfg.Defaults.RevisionIdleTimeoutSeconds)
		}
	}

	// Default ResponseStartTimeoutSeconds only in case we have a non-zero default and the latter is not larger than the revision timeout.
	// A zero default or a zero value set from the user or a nil value skips timer setup at the QP side.
	if rs.ResponseStartTimeoutSeconds == nil {
		if cfg.Defaults.RevisionResponseStartTimeoutSeconds < *rs.TimeoutSeconds && cfg.Defaults.RevisionResponseStartTimeoutSeconds != 0 {
			rs.ResponseStartTimeoutSeconds = ptr.Int64(cfg.Defaults.RevisionResponseStartTimeoutSeconds)
		}
	}

	// Default ContainerConcurrency based on our configmap.
	if rs.ContainerConcurrency == nil {
		rs.ContainerConcurrency = ptr.Int64(cfg.Defaults.ContainerConcurrency)
	}

	// Avoid clashes with user-supplied names when generating defaults.
	containerNames := make(sets.Set[string], len(rs.PodSpec.Containers)+len(rs.PodSpec.InitContainers))
	for idx := range rs.PodSpec.Containers {
		containerNames.Insert(rs.PodSpec.Containers[idx].Name)
	}
	for idx := range rs.PodSpec.InitContainers {
		containerNames.Insert(rs.PodSpec.InitContainers[idx].Name)
	}
	defaultUserContainerName := cfg.Defaults.UserContainerName(ctx)
	applyDefaultContainerNames(rs.PodSpec.Containers, containerNames, defaultUserContainerName)
	defaultInitContainerName := cfg.Defaults.InitContainerName(ctx)
	applyDefaultContainerNames(rs.PodSpec.InitContainers, containerNames, defaultInitContainerName)
	for idx := range rs.PodSpec.Containers {
		rs.applyDefault(ctx, &rs.PodSpec.Containers[idx], cfg)
		rs.defaultSecurityContext(rs.PodSpec.SecurityContext, &rs.PodSpec.Containers[idx], cfg)
	}
	for idx := range rs.PodSpec.InitContainers {
		rs.defaultSecurityContext(rs.PodSpec.SecurityContext, &rs.PodSpec.InitContainers[idx], cfg)
	}
}

func (rs *RevisionSpec) applyDefault(ctx context.Context, container *corev1.Container, cfg *config.Config) {
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
		rs.applyUserContainerDefaultReadinessProbe(container)
	}
	rs.applyReadinessProbeDefaults(container)
	rs.applyGRPCProbeDefaults(container)

	if rs.PodSpec.EnableServiceLinks == nil && apis.IsInCreate(ctx) {
		rs.PodSpec.EnableServiceLinks = cfg.Defaults.EnableServiceLinks
	}

	vNames := make(sets.Set[string])
	for _, v := range rs.PodSpec.Volumes {
		if v.EmptyDir != nil || v.PersistentVolumeClaim != nil {
			vNames.Insert(v.Name)
		}
	}
	vms := container.VolumeMounts
	for i := range vms {
		if !vNames.Has(vms[i].Name) {
			vms[i].ReadOnly = true
		}
	}
}

func (*RevisionSpec) applyUserContainerDefaultReadinessProbe(container *corev1.Container) {
	if container.ReadinessProbe == nil {
		container.ReadinessProbe = &corev1.Probe{}
	}
	if container.ReadinessProbe.TCPSocket == nil &&
		container.ReadinessProbe.HTTPGet == nil &&
		container.ReadinessProbe.Exec == nil &&
		container.ReadinessProbe.GRPC == nil {
		container.ReadinessProbe.TCPSocket = &corev1.TCPSocketAction{}
	}
}

func (*RevisionSpec) applyReadinessProbeDefaults(container *corev1.Container) {
	if container.ReadinessProbe == nil {
		// Sidecars are allowed to not have a readiness-probe
		// we do not want the defaults in that case.
		return
	}

	if container.ReadinessProbe.SuccessThreshold == 0 {
		container.ReadinessProbe.SuccessThreshold = 1
	}

	// Apply k8s defaults when ReadinessProbe.PeriodSeconds property is set
	if container.ReadinessProbe.PeriodSeconds != 0 {
		if container.ReadinessProbe.FailureThreshold == 0 {
			container.ReadinessProbe.FailureThreshold = 3
		}
		if container.ReadinessProbe.TimeoutSeconds == 0 {
			container.ReadinessProbe.TimeoutSeconds = 1
		}
	}
}

func (*RevisionSpec) applyGRPCProbeDefaults(container *corev1.Container) {
	if container.ReadinessProbe != nil && container.ReadinessProbe.GRPC != nil && container.ReadinessProbe.GRPC.Service == nil {
		container.ReadinessProbe.GRPC.Service = ptr.String("")
	}
	if container.LivenessProbe != nil && container.LivenessProbe.GRPC != nil && container.LivenessProbe.GRPC.Service == nil {
		container.LivenessProbe.GRPC.Service = ptr.String("")
	}
	if container.StartupProbe != nil && container.StartupProbe.GRPC != nil && container.StartupProbe.GRPC.Service == nil {
		container.StartupProbe.GRPC.Service = ptr.String("")
	}
}

// Upgrade SecurityContext for this container and the Pod definition to use settings
// for the `restricted` profile when the feature flag is enabled.
// when the feature flag is enabled or AllowRootBounded:
// `seccompProfile` is set to `RuntimeDefault` if its empty or nil
// `capabilities` is set to `NET_BIND_SERVICE` if its empty or nil
// when the feature flag is set to Enabled:
// `runAsNonRoot` is set to true only if its empty or nil
func (rs *RevisionSpec) defaultSecurityContext(psc *corev1.PodSecurityContext, container *corev1.Container, cfg *config.Config) {
	if !slices.Contains([]config.Flag{config.Enabled, config.AllowRootBounded}, cfg.Features.SecurePodDefaults) {
		return
	}

	if psc == nil {
		psc = &corev1.PodSecurityContext{}
	}

	updatedSC := container.SecurityContext

	if updatedSC == nil {
		updatedSC = &corev1.SecurityContext{}
	}

	if updatedSC.AllowPrivilegeEscalation == nil {
		updatedSC.AllowPrivilegeEscalation = ptr.Bool(false)
	}
	if psc.SeccompProfile == nil || psc.SeccompProfile.Type == "" {
		updatedSC.SeccompProfile = cmp.Or(updatedSC.SeccompProfile, &corev1.SeccompProfile{})
		if updatedSC.SeccompProfile.Type == "" {
			updatedSC.SeccompProfile.Type = corev1.SeccompProfileTypeRuntimeDefault
		}
	}
	if updatedSC.Capabilities == nil {
		updatedSC.Capabilities = &corev1.Capabilities{}
		updatedSC.Capabilities.Drop = []corev1.Capability{"ALL"}
		// Default in NET_BIND_SERVICE to allow binding to low-numbered ports.
		needsLowPort := false
		for _, p := range container.Ports {
			if p.ContainerPort < 1024 {
				needsLowPort = true
				break
			}
		}
		if updatedSC.Capabilities.Add == nil && needsLowPort {
			updatedSC.Capabilities.Add = []corev1.Capability{"NET_BIND_SERVICE"}
		}
	}

	if cfg.Features.SecurePodDefaults == config.Enabled {
		if psc.RunAsNonRoot == nil {
			if updatedSC.RunAsNonRoot == nil {
				updatedSC.RunAsNonRoot = ptr.Bool(true)
			}
		}
	}

	if *updatedSC != (corev1.SecurityContext{}) {
		container.SecurityContext = updatedSC
	}
}

func applyDefaultContainerNames(containers []corev1.Container, containerNames sets.Set[string], defaultContainerName string) {
	// Default container name based on ContainerNameFromTemplate value from configmap.
	// In multi-container or init-container mode, add a numeric suffix, avoiding clashes with user-supplied names.
	nextSuffix := 0
	for idx := range containers {
		if containers[idx].Name == "" {
			name := defaultContainerName

			if len(containers) > 1 || containerNames.Has(name) {
				for {
					name = kmeta.ChildName(defaultContainerName, "-"+strconv.Itoa(nextSuffix))
					nextSuffix++

					// Continue until we get a name that doesn't clash with a user-supplied name.
					if !containerNames.Has(name) {
						break
					}
				}
			}

			containers[idx].Name = name
		}
	}
}
