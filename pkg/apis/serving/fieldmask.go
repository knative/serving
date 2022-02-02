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

// Note: Please update `hack/schemapatch-config.yaml` and run `hack/update-schemas.sh` whenever
// fields are added or removed here.

package serving

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"knative.dev/serving/pkg/apis/config"
)

// VolumeMask performs a _shallow_ copy of the Kubernetes Volume object to a new
// Kubernetes Volume object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func VolumeMask(ctx context.Context, in *corev1.Volume) *corev1.Volume {
	if in == nil {
		return nil
	}
	cfg := config.FromContextOrDefaults(ctx)

	out := new(corev1.Volume)

	// Allowed fields
	out.Name = in.Name
	out.VolumeSource = in.VolumeSource

	if cfg.Features.PodSpecVolumesEmptyDir != config.Disabled {
		out.EmptyDir = in.EmptyDir
	}

	if cfg.Features.PodSpecPersistentVolumeClaim != config.Disabled {
		out.PersistentVolumeClaim = in.PersistentVolumeClaim
	}

	return out
}

// VolumeSourceMask performs a _shallow_ copy of the Kubernetes VolumeSource object to a new
// Kubernetes VolumeSource object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func VolumeSourceMask(ctx context.Context, in *corev1.VolumeSource) *corev1.VolumeSource {
	if in == nil {
		return nil
	}
	cfg := config.FromContextOrDefaults(ctx)
	out := new(corev1.VolumeSource)

	// Allowed fields
	out.Secret = in.Secret
	out.ConfigMap = in.ConfigMap
	out.Projected = in.Projected

	if cfg.Features.PodSpecVolumesEmptyDir != config.Disabled {
		out.EmptyDir = in.EmptyDir
	}

	if cfg.Features.PodSpecPersistentVolumeClaim != config.Disabled {
		out.PersistentVolumeClaim = in.PersistentVolumeClaim
	}

	// Too many disallowed fields to list

	return out
}

// VolumeProjectionMask performs a _shallow_ copy of the Kubernetes VolumeProjection
// object to a new Kubernetes VolumeProjection object bringing over only the fields allowed
// in the Knative API. This does not validate the contents or the bounds of the provided fields.
func VolumeProjectionMask(in *corev1.VolumeProjection) *corev1.VolumeProjection {
	if in == nil {
		return nil
	}

	out := new(corev1.VolumeProjection)

	// Allowed fields
	out.Secret = in.Secret
	out.ConfigMap = in.ConfigMap
	out.ServiceAccountToken = in.ServiceAccountToken

	// Disallowed fields
	// This list is unnecessary, but added here for clarity
	out.DownwardAPI = nil

	return out
}

// ConfigMapProjectionMask performs a _shallow_ copy of the Kubernetes ConfigMapProjection
// object to a new Kubernetes ConfigMapProjection object bringing over only the fields allowed
// in the Knative API. This does not validate the contents or the bounds of the provided fields.
func ConfigMapProjectionMask(in *corev1.ConfigMapProjection) *corev1.ConfigMapProjection {
	if in == nil {
		return nil
	}

	out := new(corev1.ConfigMapProjection)

	// Allowed fields
	out.LocalObjectReference = in.LocalObjectReference
	out.Items = in.Items
	out.Optional = in.Optional

	return out
}

// SecretProjectionMask performs a _shallow_ copy of the Kubernetes SecretProjection
// object to a new Kubernetes SecretProjection object bringing over only the fields allowed
// in the Knative API. This does not validate the contents or the bounds of the provided fields.
func SecretProjectionMask(in *corev1.SecretProjection) *corev1.SecretProjection {
	if in == nil {
		return nil
	}

	out := new(corev1.SecretProjection)

	// Allowed fields
	out.LocalObjectReference = in.LocalObjectReference
	out.Items = in.Items
	out.Optional = in.Optional

	return out
}

// ServiceAccountTokenProjectionMask performs a _shallow_ copy of the Kubernetes ServiceAccountTokenProjection
// object to a new Kubernetes ServiceAccountTokenProjection object bringing over only the fields allowed
// in the Knative API. This does not validate the contents or the bounds of the provided fields.
func ServiceAccountTokenProjectionMask(in *corev1.ServiceAccountTokenProjection) *corev1.ServiceAccountTokenProjection {
	if in == nil {
		return nil
	}

	out := &corev1.ServiceAccountTokenProjection{
		// Allowed fields
		Audience:          in.Audience,
		ExpirationSeconds: in.ExpirationSeconds,
		Path:              in.Path,
	}

	return out
}

// KeyToPathMask performs a _shallow_ copy of the Kubernetes KeyToPath
// object to a new Kubernetes KeyToPath object bringing over only the fields allowed
// in the Knative API. This does not validate the contents or the bounds of the provided fields.
func KeyToPathMask(in *corev1.KeyToPath) *corev1.KeyToPath {
	if in == nil {
		return nil
	}

	out := new(corev1.KeyToPath)

	// Allowed fields
	out.Key = in.Key
	out.Path = in.Path
	out.Mode = in.Mode

	return out
}

// PodSpecMask performs a _shallow_ copy of the Kubernetes PodSpec object to a new
// Kubernetes PodSpec object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func PodSpecMask(ctx context.Context, in *corev1.PodSpec) *corev1.PodSpec {
	if in == nil {
		return nil
	}

	cfg := config.FromContextOrDefaults(ctx)
	out := new(corev1.PodSpec)

	// Allowed fields
	out.ServiceAccountName = in.ServiceAccountName
	out.Containers = in.Containers
	out.Volumes = in.Volumes
	out.ImagePullSecrets = in.ImagePullSecrets
	out.EnableServiceLinks = in.EnableServiceLinks
	// Only allow setting AutomountServiceAccountToken to false
	if in.AutomountServiceAccountToken != nil && !*in.AutomountServiceAccountToken {
		out.AutomountServiceAccountToken = in.AutomountServiceAccountToken
	}

	// Feature fields
	if cfg.Features.PodSpecAffinity != config.Disabled {
		out.Affinity = in.Affinity
	}
	if cfg.Features.PodSpecHostAliases != config.Disabled {
		out.HostAliases = in.HostAliases
	}
	if cfg.Features.PodSpecNodeSelector != config.Disabled {
		out.NodeSelector = in.NodeSelector
	}
	if cfg.Features.PodSpecRuntimeClassName != config.Disabled {
		out.RuntimeClassName = in.RuntimeClassName
	}
	if cfg.Features.PodSpecTolerations != config.Disabled {
		out.Tolerations = in.Tolerations
	}
	if cfg.Features.PodSpecSecurityContext != config.Disabled {
		out.SecurityContext = in.SecurityContext
	}
	if cfg.Features.PodSpecPriorityClassName != config.Disabled {
		out.PriorityClassName = in.PriorityClassName
	}
	if cfg.Features.PodSpecSchedulerName != config.Disabled {
		out.SchedulerName = in.SchedulerName
	}
	if cfg.Features.PodSpecInitContainers != config.Disabled {
		out.InitContainers = in.InitContainers
	}

	// Disallowed fields
	// This list is unnecessary, but added here for clarity
	out.RestartPolicy = ""
	out.TerminationGracePeriodSeconds = nil
	out.ActiveDeadlineSeconds = nil
	out.DNSPolicy = ""
	out.NodeName = ""
	out.HostNetwork = false
	out.HostPID = false
	out.HostIPC = false
	out.ShareProcessNamespace = nil
	out.Hostname = ""
	out.Subdomain = ""
	out.Priority = nil
	out.DNSConfig = nil
	out.ReadinessGates = nil

	return out
}

// ContainerMask performs a _shallow_ copy of the Kubernetes Container object to a new
// Kubernetes Container object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func ContainerMask(in *corev1.Container) *corev1.Container {
	if in == nil {
		return nil
	}

	out := new(corev1.Container)

	// Allowed fields
	out.Name = in.Name
	out.Args = in.Args
	out.Command = in.Command
	out.Env = in.Env
	out.WorkingDir = in.WorkingDir
	out.EnvFrom = in.EnvFrom
	out.Image = in.Image
	out.ImagePullPolicy = in.ImagePullPolicy
	out.LivenessProbe = in.LivenessProbe
	out.Ports = in.Ports
	out.ReadinessProbe = in.ReadinessProbe
	out.Resources = in.Resources
	out.SecurityContext = in.SecurityContext
	out.TerminationMessagePath = in.TerminationMessagePath
	out.TerminationMessagePolicy = in.TerminationMessagePolicy
	out.VolumeMounts = in.VolumeMounts

	// Disallowed fields
	// This list is unnecessary, but added here for clarity
	out.Lifecycle = nil
	out.Stdin = false
	out.StdinOnce = false
	out.TTY = false
	out.VolumeDevices = nil

	return out
}

// VolumeMountMask performs a _shallow_ copy of the Kubernetes VolumeMount object to a new
// Kubernetes VolumeMount object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func VolumeMountMask(in *corev1.VolumeMount) *corev1.VolumeMount {
	if in == nil {
		return nil
	}

	out := new(corev1.VolumeMount)

	// Allowed fields
	out.Name = in.Name
	out.ReadOnly = in.ReadOnly
	out.MountPath = in.MountPath
	out.SubPath = in.SubPath

	// Disallowed fields
	// This list is unnecessary, but added here for clarity
	out.MountPropagation = nil

	return out
}

// ProbeMask performs a _shallow_ copy of the Kubernetes Probe object to a new
// Kubernetes Probe object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func ProbeMask(in *corev1.Probe) *corev1.Probe {
	if in == nil {
		return nil
	}
	out := new(corev1.Probe)

	// Allowed fields
	out.Handler = in.Handler
	out.InitialDelaySeconds = in.InitialDelaySeconds
	out.TimeoutSeconds = in.TimeoutSeconds
	out.PeriodSeconds = in.PeriodSeconds
	out.SuccessThreshold = in.SuccessThreshold
	out.FailureThreshold = in.FailureThreshold

	return out
}

// HandlerMask performs a _shallow_ copy of the Kubernetes Handler object to a new
// Kubernetes Handler object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func HandlerMask(in *corev1.Handler) *corev1.Handler {
	if in == nil {
		return nil
	}
	out := new(corev1.Handler)

	// Allowed fields
	out.Exec = in.Exec
	out.HTTPGet = in.HTTPGet
	out.TCPSocket = in.TCPSocket

	return out

}

// ExecActionMask performs a _shallow_ copy of the Kubernetes ExecAction object to a new
// Kubernetes ExecAction object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func ExecActionMask(in *corev1.ExecAction) *corev1.ExecAction {
	if in == nil {
		return nil
	}
	out := new(corev1.ExecAction)

	// Allowed fields
	out.Command = in.Command

	return out
}

// HTTPGetActionMask performs a _shallow_ copy of the Kubernetes HTTPGetAction object to a new
// Kubernetes HTTPGetAction object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func HTTPGetActionMask(in *corev1.HTTPGetAction) *corev1.HTTPGetAction {
	if in == nil {
		return nil
	}
	out := new(corev1.HTTPGetAction)

	// Allowed fields
	out.Host = in.Host
	out.Path = in.Path
	out.Scheme = in.Scheme
	out.HTTPHeaders = in.HTTPHeaders
	out.Port = in.Port

	return out
}

// TCPSocketActionMask performs a _shallow_ copy of the Kubernetes TCPSocketAction object to a new
// Kubernetes TCPSocketAction object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func TCPSocketActionMask(in *corev1.TCPSocketAction) *corev1.TCPSocketAction {
	if in == nil {
		return nil
	}
	out := new(corev1.TCPSocketAction)

	// Allowed fields
	out.Host = in.Host
	out.Port = in.Port

	return out
}

// ContainerPortMask performs a _shallow_ copy of the Kubernetes ContainerPort object to a new
// Kubernetes ContainerPort object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func ContainerPortMask(in *corev1.ContainerPort) *corev1.ContainerPort {
	if in == nil {
		return nil
	}

	out := new(corev1.ContainerPort)

	// Allowed fields
	out.ContainerPort = in.ContainerPort
	out.Name = in.Name
	out.Protocol = in.Protocol

	//Disallowed fields
	// This list is unnecessary, but added here for clarity
	out.HostIP = ""
	out.HostPort = 0

	return out
}

// EnvVarMask performs a _shallow_ copy of the Kubernetes EnvVar object to a new
// Kubernetes EnvVar object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func EnvVarMask(in *corev1.EnvVar) *corev1.EnvVar {
	if in == nil {
		return nil
	}

	out := new(corev1.EnvVar)

	// Allowed fields
	out.Name = in.Name
	out.Value = in.Value
	out.ValueFrom = in.ValueFrom

	return out
}

// EnvVarSourceMask performs a _shallow_ copy of the Kubernetes EnvVarSource object to a new
// Kubernetes EnvVarSource object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func EnvVarSourceMask(in *corev1.EnvVarSource, fieldRef bool) *corev1.EnvVarSource {
	if in == nil {
		return nil
	}

	out := new(corev1.EnvVarSource)

	// Allowed fields
	out.ConfigMapKeyRef = in.ConfigMapKeyRef
	out.SecretKeyRef = in.SecretKeyRef

	if fieldRef {
		out.FieldRef = in.FieldRef
		out.ResourceFieldRef = in.ResourceFieldRef
	}

	return out
}

// LocalObjectReferenceMask performs a _shallow_ copy of the Kubernetes LocalObjectReference object to a new
// Kubernetes LocalObjectReference object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func LocalObjectReferenceMask(in *corev1.LocalObjectReference) *corev1.LocalObjectReference {
	if in == nil {
		return nil
	}

	out := new(corev1.LocalObjectReference)

	out.Name = in.Name

	return out
}

// ConfigMapKeySelectorMask performs a _shallow_ copy of the Kubernetes ConfigMapKeySelector object to a new
// Kubernetes ConfigMapKeySelector object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func ConfigMapKeySelectorMask(in *corev1.ConfigMapKeySelector) *corev1.ConfigMapKeySelector {
	if in == nil {
		return nil
	}

	out := new(corev1.ConfigMapKeySelector)

	// Allowed fields
	out.Key = in.Key
	out.Optional = in.Optional
	out.LocalObjectReference = in.LocalObjectReference

	return out

}

// SecretKeySelectorMask performs a _shallow_ copy of the Kubernetes SecretKeySelector object to a new
// Kubernetes SecretKeySelector object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func SecretKeySelectorMask(in *corev1.SecretKeySelector) *corev1.SecretKeySelector {
	if in == nil {
		return nil
	}

	out := new(corev1.SecretKeySelector)

	// Allowed fields
	out.Key = in.Key
	out.Optional = in.Optional
	out.LocalObjectReference = in.LocalObjectReference

	return out

}

// ConfigMapEnvSourceMask performs a _shallow_ copy of the Kubernetes ConfigMapEnvSource object to a new
// Kubernetes ConfigMapEnvSource object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func ConfigMapEnvSourceMask(in *corev1.ConfigMapEnvSource) *corev1.ConfigMapEnvSource {
	if in == nil {
		return nil
	}

	out := new(corev1.ConfigMapEnvSource)

	// Allowed fields
	out.Optional = in.Optional
	out.LocalObjectReference = in.LocalObjectReference

	return out

}

// SecretEnvSourceMask performs a _shallow_ copy of the Kubernetes SecretEnvSource object to a new
// Kubernetes SecretEnvSource object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func SecretEnvSourceMask(in *corev1.SecretEnvSource) *corev1.SecretEnvSource {
	if in == nil {
		return nil
	}

	out := new(corev1.SecretEnvSource)

	// Allowed fields
	out.Optional = in.Optional
	out.LocalObjectReference = in.LocalObjectReference

	return out

}

// EnvFromSourceMask performs a _shallow_ copy of the Kubernetes EnvFromSource object to a new
// Kubernetes EnvFromSource object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func EnvFromSourceMask(in *corev1.EnvFromSource) *corev1.EnvFromSource {
	if in == nil {
		return nil
	}

	out := new(corev1.EnvFromSource)

	// Allowed fields
	out.Prefix = in.Prefix
	out.ConfigMapRef = in.ConfigMapRef
	out.SecretRef = in.SecretRef

	return out
}

// ResourceRequirementsMask performs a _shallow_ copy of the Kubernetes ResourceRequirements object to a new
// Kubernetes ResourceRequirements object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func ResourceRequirementsMask(in *corev1.ResourceRequirements) *corev1.ResourceRequirements {
	if in == nil {
		return nil
	}

	out := new(corev1.ResourceRequirements)

	// Allowed fields
	out.Limits = in.Limits
	out.Requests = in.Requests

	return out

}

// PodSecurityContextMask performs a _shallow_ copy of the Kubernetes PodSecurityContext object into a new
// Kubernetes PodSecurityContext object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or bounds of the provided fields.
func PodSecurityContextMask(ctx context.Context, in *corev1.PodSecurityContext) *corev1.PodSecurityContext {
	if in == nil {
		return nil
	}

	out := new(corev1.PodSecurityContext)

	if config.FromContextOrDefaults(ctx).Features.PodSpecSecurityContext == config.Disabled {
		return out
	}

	out.RunAsUser = in.RunAsUser
	out.RunAsGroup = in.RunAsGroup
	out.RunAsNonRoot = in.RunAsNonRoot
	out.FSGroup = in.FSGroup
	out.SupplementalGroups = in.SupplementalGroups

	// Disallowed
	// This list is unnecessary, but added here for clarity
	out.SELinuxOptions = nil
	out.WindowsOptions = nil
	out.Sysctls = nil

	return out
}

// SecurityContextMask performs a _shallow_ copy of the Kubernetes SecurityContext object to a new
// Kubernetes SecurityContext object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func SecurityContextMask(ctx context.Context, in *corev1.SecurityContext) *corev1.SecurityContext {
	if in == nil {
		return nil
	}

	out := new(corev1.SecurityContext)

	// Allowed fields
	out.Capabilities = in.Capabilities
	out.ReadOnlyRootFilesystem = in.ReadOnlyRootFilesystem
	out.RunAsUser = in.RunAsUser
	out.RunAsGroup = in.RunAsGroup
	// RunAsNonRoot when unset behaves the same way as false
	// We do want the ability for folks to set this value to true
	out.RunAsNonRoot = in.RunAsNonRoot

	// Disallowed
	// This list is unnecessary, but added here for clarity
	out.Privileged = nil
	out.SELinuxOptions = nil
	out.AllowPrivilegeEscalation = nil
	out.ProcMount = nil

	return out
}

// CapabilitiesMask performs a _shallow_ copy of the Kubernetes Capabilities object to a new
// Kubernetes Capabilities object bringing over only the fields allowed in the Knative API. This
// does not validate the contents or the bounds of the provided fields.
func CapabilitiesMask(ctx context.Context, in *corev1.Capabilities) *corev1.Capabilities {
	if in == nil {
		return nil
	}

	out := new(corev1.Capabilities)

	// Allowed fields
	out.Drop = in.Drop

	if config.FromContextOrDefaults(ctx).Features.ContainerSpecAddCapabilities != config.Disabled {
		out.Add = in.Add
	}

	return out
}

// NamespacedObjectReferenceMask performs a _shallow_ copy of the Kubernetes ObjectReference
// object to a new Kubernetes ObjectReference object bringing over only the fields allowed in
// the Knative API. This does not validate the contents or the bounds of the provided fields.
func NamespacedObjectReferenceMask(in *corev1.ObjectReference) *corev1.ObjectReference {
	if in == nil {
		return nil
	}

	out := new(corev1.ObjectReference)

	// Allowed fields
	out.APIVersion = in.APIVersion
	out.Kind = in.Kind
	out.Name = in.Name

	// Disallowed
	// This list is unnecessary, but added here for clarity
	out.Namespace = ""
	out.FieldPath = ""
	out.ResourceVersion = ""
	out.UID = ""

	return out
}
