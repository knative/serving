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
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/profiling"
	"knative.dev/serving/pkg/apis/config"
)

const (
	minUserID, maxUserID   = 0, math.MaxInt32
	minGroupID, maxGroupID = 0, math.MaxInt32
)

var (
	reservedPaths = sets.NewString(
		"/",
		"/dev",
		"/dev/log", // Should be a domain socket
		"/tmp",
		"/var",
		"/var/log",
	)

	reservedContainerNames = sets.NewString(
		"queue-proxy",
	)

	reservedEnvVars = sets.NewString(
		"PORT",
		"K_SERVICE",
		"K_CONFIGURATION",
		"K_REVISION",
	)

	reservedPorts = sets.NewInt32(
		networking.BackendHTTPPort,
		networking.BackendHTTP2Port,
		networking.QueueAdminPort,
		networking.AutoscalingQueueMetricsPort,
		networking.UserQueueMetricsPort,
		profiling.ProfilingPort)

	reservedSidecarEnvVars = reservedEnvVars.Difference(sets.NewString("PORT"))

	// The port is named "user-port" on the deployment, but a user cannot set an arbitrary name on the port
	// in Configuration. The name field is reserved for content-negotiation. Currently 'h2c' and 'http1' are
	// allowed.
	// https://github.com/knative/serving/blob/master/docs/runtime-contract.md#inbound-network-connectivity
	validPortNames = sets.NewString(
		"h2c",
		"http1",
		"",
	)
)

func ValidateVolumes(vs []corev1.Volume, mountedVolumes sets.String) (sets.String, *apis.FieldError) {
	volumes := make(sets.String, len(vs))
	var errs *apis.FieldError
	for i, volume := range vs {
		if volumes.Has(volume.Name) {
			errs = errs.Also((&apis.FieldError{
				Message: fmt.Sprintf("duplicate volume name %q", volume.Name),
				Paths:   []string{"name"},
			}).ViaIndex(i))
		}
		if !mountedVolumes.Has(volume.Name) {
			errs = errs.Also((&apis.FieldError{
				Message: fmt.Sprintf("volume with name %q not mounted", volume.Name),
				Paths:   []string{"name"},
			}).ViaIndex(i))
		}
		errs = errs.Also(validateVolume(volume).ViaIndex(i))
		volumes.Insert(volume.Name)
	}
	return volumes, errs
}

func validateVolume(volume corev1.Volume) *apis.FieldError {
	errs := apis.CheckDisallowedFields(volume, *VolumeMask(&volume))
	if volume.Name == "" {
		errs = apis.ErrMissingField("name")
	} else if len(validation.IsDNS1123Label(volume.Name)) != 0 {
		errs = apis.ErrInvalidValue(volume.Name, "name")
	}

	vs := volume.VolumeSource
	errs = errs.Also(apis.CheckDisallowedFields(vs, *VolumeSourceMask(&vs)))
	specified := []string{}
	if vs.Secret != nil {
		specified = append(specified, "secret")
		for i, item := range vs.Secret.Items {
			errs = errs.Also(validateKeyToPath(item).ViaFieldIndex("items", i))
		}
	}
	if vs.ConfigMap != nil {
		specified = append(specified, "configMap")
		for i, item := range vs.ConfigMap.Items {
			errs = errs.Also(validateKeyToPath(item).ViaFieldIndex("items", i))
		}
	}
	if vs.Projected != nil {
		specified = append(specified, "projected")
		for i, proj := range vs.Projected.Sources {
			errs = errs.Also(validateProjectedVolumeSource(proj).ViaFieldIndex("projected", i))
		}
	}
	if len(specified) == 0 {
		errs = errs.Also(apis.ErrMissingOneOf("secret", "configMap", "projected"))
	} else if len(specified) > 1 {
		errs = errs.Also(apis.ErrMultipleOneOf(specified...))
	}

	return errs
}

func validateProjectedVolumeSource(vp corev1.VolumeProjection) *apis.FieldError {
	errs := apis.CheckDisallowedFields(vp, *VolumeProjectionMask(&vp))
	specified := make([]string, 0, 1) // Most of the time there will be a success with a single element.
	if vp.Secret != nil {
		specified = append(specified, "secret")
		errs = errs.Also(validateSecretProjection(vp.Secret).ViaField("secret"))
	}
	if vp.ConfigMap != nil {
		specified = append(specified, "configMap")
		errs = errs.Also(validateConfigMapProjection(vp.ConfigMap).ViaField("configMap"))
	}
	if vp.ServiceAccountToken != nil {
		specified = append(specified, "serviceAccountToken")
		errs = errs.Also(validateServiceAccountTokenProjection(vp.ServiceAccountToken).ViaField("serviceAccountToken"))
	}
	if len(specified) == 0 {
		errs = errs.Also(apis.ErrMissingOneOf("secret", "configMap", "serviceAccountToken"))
	} else if len(specified) > 1 {
		errs = errs.Also(apis.ErrMultipleOneOf(specified...))
	}
	return errs
}

func validateConfigMapProjection(cmp *corev1.ConfigMapProjection) *apis.FieldError {
	errs := apis.CheckDisallowedFields(*cmp, *ConfigMapProjectionMask(cmp)).
		Also(apis.CheckDisallowedFields(
			cmp.LocalObjectReference, *LocalObjectReferenceMask(&cmp.LocalObjectReference)))
	if cmp.Name == "" {
		errs = errs.Also(apis.ErrMissingField("name"))
	}
	for i, item := range cmp.Items {
		errs = errs.Also(validateKeyToPath(item).ViaFieldIndex("items", i))
	}
	return errs
}

func validateSecretProjection(sp *corev1.SecretProjection) *apis.FieldError {
	errs := apis.CheckDisallowedFields(*sp, *SecretProjectionMask(sp)).
		Also(apis.CheckDisallowedFields(
			sp.LocalObjectReference, *LocalObjectReferenceMask(&sp.LocalObjectReference)))
	if sp.Name == "" {
		errs = errs.Also(apis.ErrMissingField("name"))
	}
	for i, item := range sp.Items {
		errs = errs.Also(validateKeyToPath(item).ViaFieldIndex("items", i))
	}
	return errs
}

func validateServiceAccountTokenProjection(sp *corev1.ServiceAccountTokenProjection) *apis.FieldError {
	errs := apis.CheckDisallowedFields(*sp, *ServiceAccountTokenProjectionMask(sp))
	// Audience & ExpirationSeconds are optional
	if sp.Path == "" {
		errs = errs.Also(apis.ErrMissingField("path"))
	}
	return errs
}

func validateKeyToPath(k2p corev1.KeyToPath) *apis.FieldError {
	errs := apis.CheckDisallowedFields(k2p, *KeyToPathMask(&k2p))
	if k2p.Key == "" {
		errs = errs.Also(apis.ErrMissingField("key"))
	}
	if k2p.Path == "" {
		errs = errs.Also(apis.ErrMissingField("path"))
	}
	return errs
}

func validateEnvValueFrom(ctx context.Context, source *corev1.EnvVarSource) *apis.FieldError {
	if source == nil {
		return nil
	}
	features := config.FromContextOrDefaults(ctx).Features
	return apis.CheckDisallowedFields(*source, *EnvVarSourceMask(source, features.PodSpecFieldRef != config.Disabled))
}

func getReservedEnvVarsPerContainerType(ctx context.Context) sets.String {
	if IsInSidecarContainer(ctx) {
		return reservedSidecarEnvVars
	}
	return reservedEnvVars
}

func validateEnvVar(ctx context.Context, env corev1.EnvVar) *apis.FieldError {
	errs := apis.CheckDisallowedFields(env, *EnvVarMask(&env))

	if env.Name == "" {
		errs = errs.Also(apis.ErrMissingField("name"))
	} else if getReservedEnvVarsPerContainerType(ctx).Has(env.Name) {
		errs = errs.Also(&apis.FieldError{
			Message: fmt.Sprintf("%q is a reserved environment variable", env.Name),
			Paths:   []string{"name"},
		})
	}

	return errs.Also(validateEnvValueFrom(ctx, env.ValueFrom).ViaField("valueFrom"))
}

func validateEnv(ctx context.Context, envVars []corev1.EnvVar) *apis.FieldError {
	var errs *apis.FieldError
	for i, env := range envVars {
		errs = errs.Also(validateEnvVar(ctx, env).ViaIndex(i))
	}
	return errs
}

func validateEnvFrom(envFromList []corev1.EnvFromSource) *apis.FieldError {
	var errs *apis.FieldError
	for i := range envFromList {
		envFrom := envFromList[i]
		errs = errs.Also(apis.CheckDisallowedFields(envFrom, *EnvFromSourceMask(&envFrom)).ViaIndex(i))

		cm := envFrom.ConfigMapRef
		sm := envFrom.SecretRef
		if sm != nil {
			errs = errs.Also(apis.CheckDisallowedFields(*sm, *SecretEnvSourceMask(sm)).ViaIndex(i))
			errs = errs.Also(apis.CheckDisallowedFields(
				sm.LocalObjectReference, *LocalObjectReferenceMask(&sm.LocalObjectReference))).ViaIndex(i).ViaField("secretRef")
		}

		if cm != nil {
			errs = errs.Also(apis.CheckDisallowedFields(*cm, *ConfigMapEnvSourceMask(cm)).ViaIndex(i))
			errs = errs.Also(apis.CheckDisallowedFields(
				cm.LocalObjectReference, *LocalObjectReferenceMask(&cm.LocalObjectReference))).ViaIndex(i).ViaField("configMapRef")
		}
		if cm != nil && sm != nil {
			errs = errs.Also(apis.ErrMultipleOneOf("configMapRef", "secretRef"))
		} else if cm == nil && sm == nil {
			errs = errs.Also(apis.ErrMissingOneOf("configMapRef", "secretRef"))
		}
	}
	return errs
}

// ValidatePodSpec validates the pod spec
func ValidatePodSpec(ctx context.Context, ps corev1.PodSpec) *apis.FieldError {
	// This is inlined, and so it makes for a less meaningful
	// error message.
	// if equality.Semantic.DeepEqual(ps, corev1.PodSpec{}) {
	// 	return apis.ErrMissingField(apis.CurrentField)
	// }

	errs := apis.CheckDisallowedFields(ps, *PodSpecMask(ctx, &ps))

	errs = errs.Also(ValidatePodSecurityContext(ctx, ps.SecurityContext).ViaField("securityContext"))

	volumes, err := ValidateVolumes(ps.Volumes, AllMountedVolumes(ps.Containers))
	if err != nil {
		errs = errs.Also(err.ViaField("volumes"))
	}

	switch len(ps.Containers) {
	case 0:
		errs = errs.Also(apis.ErrMissingField("containers"))
	case 1:
		errs = errs.Also(ValidateContainer(ctx, ps.Containers[0], volumes).
			ViaFieldIndex("containers", 0))
	default:
		errs = errs.Also(validateContainers(ctx, ps.Containers, volumes))
	}
	if ps.ServiceAccountName != "" {
		for range validation.IsDNS1123Subdomain(ps.ServiceAccountName) {
			errs = errs.Also(apis.ErrInvalidValue("serviceAccountName", ps.ServiceAccountName))
		}
	}
	return errs
}

func validateContainers(ctx context.Context, containers []corev1.Container, volumes sets.String) (errs *apis.FieldError) {
	features := config.FromContextOrDefaults(ctx).Features
	if features.MultiContainer != config.Enabled {
		return errs.Also(&apis.FieldError{Message: fmt.Sprintf("multi-container is off, "+
			"but found %d containers", len(containers))})
	}
	errs = errs.Also(validateContainersPorts(containers).ViaField("containers"))
	for i := range containers {
		// Probes are not allowed on other than serving container,
		// ref: http://bit.ly/probes-condition
		if len(containers[i].Ports) == 0 {
			errs = errs.Also(validateSidecarContainer(WithinSidecarContainer(ctx), containers[i], volumes).ViaFieldIndex("containers", i))
		} else {
			errs = errs.Also(ValidateContainer(WithinUserContainer(ctx), containers[i], volumes).ViaFieldIndex("containers", i))
		}
	}
	return errs
}

// AllMountedVolumes returns all the mounted volumes in all the containers.
func AllMountedVolumes(containers []corev1.Container) sets.String {
	volumeNames := sets.NewString()
	for _, c := range containers {
		for _, vm := range c.VolumeMounts {
			volumeNames.Insert(vm.Name)
		}
	}
	return volumeNames
}

// validateContainersPorts validates port when specified multiple containers
func validateContainersPorts(containers []corev1.Container) *apis.FieldError {
	var count int
	for i := range containers {
		count += len(containers[i].Ports)
	}
	// When no container ports are specified.
	if count == 0 {
		return apis.ErrMissingField("ports")
	}
	// More than one container sections have ports.
	if count > 1 {
		return apis.ErrMultipleOneOf("ports")
	}
	return nil
}

// validateSidecarContainer validate fields for non serving containers
func validateSidecarContainer(ctx context.Context, container corev1.Container, volumes sets.String) (errs *apis.FieldError) {
	if container.LivenessProbe != nil {
		errs = errs.Also(apis.CheckDisallowedFields(*container.LivenessProbe,
			*ProbeMask(&corev1.Probe{})).ViaField("livenessProbe"))
	}
	if container.ReadinessProbe != nil {
		errs = errs.Also(apis.CheckDisallowedFields(*container.ReadinessProbe,
			*ProbeMask(&corev1.Probe{})).ViaField("readinessProbe"))
	}
	return errs.Also(validate(ctx, container, volumes))
}

// ValidateContainer validate fields for serving containers
func ValidateContainer(ctx context.Context, container corev1.Container, volumes sets.String) (errs *apis.FieldError) {
	// Single container cannot have multiple ports
	errs = errs.Also(portValidation(container.Ports).ViaField("ports"))
	// Liveness Probes
	errs = errs.Also(validateProbe(container.LivenessProbe).ViaField("livenessProbe"))
	// Readiness Probes
	errs = errs.Also(validateReadinessProbe(container.ReadinessProbe).ViaField("readinessProbe"))
	return errs.Also(validate(ctx, container, volumes))
}

func portValidation(containerPorts []corev1.ContainerPort) *apis.FieldError {
	if len(containerPorts) > 1 {
		return &apis.FieldError{
			Message: "More than one container port is set",
			Paths:   []string{apis.CurrentField},
			Details: "Only a single port is allowed",
		}
	}
	return nil
}

func validate(ctx context.Context, container corev1.Container, volumes sets.String) *apis.FieldError {
	if equality.Semantic.DeepEqual(container, corev1.Container{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}

	errs := apis.CheckDisallowedFields(container, *ContainerMask(&container))

	if reservedContainerNames.Has(container.Name) {
		errs = errs.Also(&apis.FieldError{
			Message: fmt.Sprintf("%q is a reserved container name", container.Name),
			Paths:   []string{"name"},
		})
	}

	// Env
	errs = errs.Also(validateEnv(ctx, container.Env).ViaField("env"))
	// EnvFrom
	errs = errs.Also(validateEnvFrom(container.EnvFrom).ViaField("envFrom"))
	// Image
	if container.Image == "" {
		errs = errs.Also(apis.ErrMissingField("image"))
	} else if _, err := name.ParseReference(container.Image, name.WeakValidation); err != nil {
		fe := &apis.FieldError{
			Message: "Failed to parse image reference",
			Paths:   []string{"image"},
			Details: fmt.Sprintf("image: %q, error: %v", container.Image, err),
		}
		errs = errs.Also(fe)
	}
	// Ports
	errs = errs.Also(validateContainerPorts(container.Ports).ViaField("ports"))
	// Resources
	errs = errs.Also(validateResources(&container.Resources).ViaField("resources"))
	// SecurityContext
	errs = errs.Also(validateSecurityContext(ctx, container.SecurityContext).ViaField("securityContext"))
	// TerminationMessagePolicy
	switch container.TerminationMessagePolicy {
	case corev1.TerminationMessageReadFile, corev1.TerminationMessageFallbackToLogsOnError, "":
	default:
		errs = errs.Also(apis.ErrInvalidValue(container.TerminationMessagePolicy, "terminationMessagePolicy"))
	}
	// VolumeMounts
	errs = errs.Also(validateVolumeMounts(container.VolumeMounts, volumes).ViaField("volumeMounts"))

	return errs
}

func validateResources(resources *corev1.ResourceRequirements) *apis.FieldError {
	if resources == nil {
		return nil
	}
	return apis.CheckDisallowedFields(*resources, *ResourceRequirementsMask(resources))
}

func validateSecurityContext(ctx context.Context, sc *corev1.SecurityContext) *apis.FieldError {
	if sc == nil {
		return nil
	}
	errs := apis.CheckDisallowedFields(*sc, *SecurityContextMask(ctx, sc))

	if sc.RunAsUser != nil {
		uid := *sc.RunAsUser
		if uid < minUserID || uid > maxUserID {
			errs = errs.Also(apis.ErrOutOfBoundsValue(uid, minUserID, maxUserID, "runAsUser"))
		}
	}

	if sc.RunAsGroup != nil {
		gid := *sc.RunAsGroup
		if gid < minGroupID || gid > maxGroupID {
			errs = errs.Also(apis.ErrOutOfBoundsValue(gid, minGroupID, maxGroupID, "runAsGroup"))
		}
	}
	return errs
}

func validateVolumeMounts(mounts []corev1.VolumeMount, volumes sets.String) *apis.FieldError {
	var errs *apis.FieldError
	// Check that volume mounts match names in "volumes", that "volumes" has 100%
	// coverage, and the field restrictions.
	seenName := make(sets.String, len(mounts))
	seenMountPath := make(sets.String, len(mounts))
	for i := range mounts {
		vm := mounts[i]
		errs = errs.Also(apis.CheckDisallowedFields(vm, *VolumeMountMask(&vm)).ViaIndex(i))
		// This effectively checks that Name is non-empty because Volume name must be non-empty.
		if !volumes.Has(vm.Name) {
			errs = errs.Also((&apis.FieldError{
				Message: "volumeMount has no matching volume",
				Paths:   []string{"name"},
			}).ViaIndex(i))
		}
		seenName.Insert(vm.Name)

		if vm.MountPath == "" {
			errs = errs.Also(apis.ErrMissingField("mountPath").ViaIndex(i))
		} else if reservedPaths.Has(filepath.Clean(vm.MountPath)) {
			errs = errs.Also((&apis.FieldError{
				Message: fmt.Sprintf("mountPath %q is a reserved path", filepath.Clean(vm.MountPath)),
				Paths:   []string{"mountPath"},
			}).ViaIndex(i))
		} else if !filepath.IsAbs(vm.MountPath) {
			errs = errs.Also(apis.ErrInvalidValue(vm.MountPath, "mountPath").ViaIndex(i))
		} else if seenMountPath.Has(filepath.Clean(vm.MountPath)) {
			errs = errs.Also(apis.ErrInvalidValue(
				fmt.Sprintf("%q must be unique", vm.MountPath), "mountPath").ViaIndex(i))
		}
		seenMountPath.Insert(filepath.Clean(vm.MountPath))

		if !vm.ReadOnly {
			errs = errs.Also(apis.ErrMissingField("readOnly").ViaIndex(i))
		}

	}
	return errs
}

func validateContainerPorts(ports []corev1.ContainerPort) *apis.FieldError {
	if len(ports) == 0 {
		return nil
	}

	var errs *apis.FieldError

	// user can set container port which names "user-port" to define application's port.
	// Queue-proxy will use it to send requests to application
	// if user didn't set any port, it will set default port user-port=8080.
	userPort := ports[0]

	errs = errs.Also(apis.CheckDisallowedFields(userPort, *ContainerPortMask(&userPort)))

	// Only allow empty (defaulting to "TCP") or explicit TCP for protocol
	if userPort.Protocol != "" && userPort.Protocol != corev1.ProtocolTCP {
		errs = errs.Also(apis.ErrInvalidValue(userPort.Protocol, "protocol"))
	}

	// Don't allow userPort to conflict with knative system reserved ports
	if reservedPorts.Has(userPort.ContainerPort) {
		errs = errs.Also(apis.ErrInvalidValue(userPort.ContainerPort, "containerPort"))
	}

	if userPort.ContainerPort < 0 || userPort.ContainerPort > 65535 {
		errs = errs.Also(apis.ErrOutOfBoundsValue(userPort.ContainerPort,
			0, 65535, "containerPort"))
	}

	if !validPortNames.Has(userPort.Name) {
		errs = errs.Also(&apis.FieldError{
			Message: fmt.Sprintf("Port name %v is not allowed", ports[0].Name),
			Paths:   []string{apis.CurrentField},
			Details: "Name must be empty, or one of: 'h2c', 'http1'",
		})
	}

	return errs
}

func validateReadinessProbe(p *corev1.Probe) *apis.FieldError {
	if p == nil {
		return nil
	}

	errs := validateProbe(p)

	if p.PeriodSeconds < 0 {
		errs = errs.Also(apis.ErrOutOfBoundsValue(p.PeriodSeconds, 0, math.MaxInt32, "periodSeconds"))
	}

	if p.InitialDelaySeconds < 0 {
		errs = errs.Also(apis.ErrOutOfBoundsValue(p.InitialDelaySeconds, 0, math.MaxInt32, "initialDelaySeconds"))
	}

	if p.SuccessThreshold < 1 {
		errs = errs.Also(apis.ErrOutOfBoundsValue(p.SuccessThreshold, 1, math.MaxInt32, "successThreshold"))
	}

	// PeriodSeconds == 0 indicates Knative's special probe with aggressive retries
	if p.PeriodSeconds == 0 {
		if p.FailureThreshold != 0 {
			errs = errs.Also(&apis.FieldError{
				Message: "failureThreshold is disallowed when periodSeconds is zero",
				Paths:   []string{"failureThreshold"},
			})
		}

		if p.TimeoutSeconds != 0 {
			errs = errs.Also(&apis.FieldError{
				Message: "timeoutSeconds is disallowed when periodSeconds is zero",
				Paths:   []string{"timeoutSeconds"},
			})
		}
	} else {
		if p.TimeoutSeconds < 1 {
			errs = errs.Also(apis.ErrOutOfBoundsValue(p.TimeoutSeconds, 1, math.MaxInt32, "timeoutSeconds"))
		}

		if p.FailureThreshold < 1 {
			errs = errs.Also(apis.ErrOutOfBoundsValue(p.FailureThreshold, 1, math.MaxInt32, "failureThreshold"))
		}
	}

	return errs
}

func validateProbe(p *corev1.Probe) *apis.FieldError {
	if p == nil {
		return nil
	}
	errs := apis.CheckDisallowedFields(*p, *ProbeMask(p))

	h := p.Handler
	errs = errs.Also(apis.CheckDisallowedFields(h, *HandlerMask(&h)))

	var handlers []string

	if h.HTTPGet != nil {
		handlers = append(handlers, "httpGet")
		errs = errs.Also(apis.CheckDisallowedFields(*h.HTTPGet, *HTTPGetActionMask(h.HTTPGet))).ViaField("httpGet")
	}
	if h.TCPSocket != nil {
		handlers = append(handlers, "tcpSocket")
		errs = errs.Also(apis.CheckDisallowedFields(*h.TCPSocket, *TCPSocketActionMask(h.TCPSocket))).ViaField("tcpSocket")
	}
	if h.Exec != nil {
		handlers = append(handlers, "exec")
		errs = errs.Also(apis.CheckDisallowedFields(*h.Exec, *ExecActionMask(h.Exec))).ViaField("exec")
	}

	if len(handlers) == 0 {
		errs = errs.Also(apis.ErrMissingOneOf("httpGet", "tcpSocket", "exec"))
	} else if len(handlers) > 1 {
		errs = errs.Also(apis.ErrMultipleOneOf(handlers...))
	}
	return errs
}

func ValidateNamespacedObjectReference(p *corev1.ObjectReference) *apis.FieldError {
	if p == nil {
		return nil
	}
	errs := apis.CheckDisallowedFields(*p, *NamespacedObjectReferenceMask(p))

	if p.APIVersion == "" {
		errs = errs.Also(apis.ErrMissingField("apiVersion"))
	} else if verrs := validation.IsQualifiedName(p.APIVersion); len(verrs) != 0 {
		errs = errs.Also(apis.ErrInvalidValue(strings.Join(verrs, ", "), "apiVersion"))
	}
	if p.Kind == "" {
		errs = errs.Also(apis.ErrMissingField("kind"))
	} else if verrs := validation.IsCIdentifier(p.Kind); len(verrs) != 0 {
		errs = errs.Also(apis.ErrInvalidValue(strings.Join(verrs, ", "), "kind"))
	}
	if p.Name == "" {
		errs = errs.Also(apis.ErrMissingField("name"))
	} else if verrs := validation.IsDNS1123Label(p.Name); len(verrs) != 0 {
		errs = errs.Also(apis.ErrInvalidValue(strings.Join(verrs, ", "), "name"))
	}
	return errs
}

// ValidatePodSecurityContext validates the PodSecurityContext struct. All fields are disallowed
// unless the 'PodSpecSecurityContext' feature flag is enabled
//
// See the allowed properties in the `PodSecurityContextMask`
func ValidatePodSecurityContext(ctx context.Context, sc *corev1.PodSecurityContext) *apis.FieldError {
	if sc == nil {
		return nil
	}

	errs := apis.CheckDisallowedFields(*sc, *PodSecurityContextMask(ctx, sc))

	if sc.RunAsUser != nil {
		uid := *sc.RunAsUser
		if uid < minUserID || uid > maxUserID {
			errs = errs.Also(apis.ErrOutOfBoundsValue(uid, minUserID, maxUserID, "runAsUser"))
		}
	}

	if sc.RunAsGroup != nil {
		gid := *sc.RunAsGroup
		if gid < minGroupID || gid > maxGroupID {
			errs = errs.Also(apis.ErrOutOfBoundsValue(gid, minGroupID, maxGroupID, "runAsGroup"))
		}
	}

	if sc.FSGroup != nil {
		gid := *sc.FSGroup
		if gid < minGroupID || gid > maxGroupID {
			errs = errs.Also(apis.ErrOutOfBoundsValue(gid, minGroupID, maxGroupID, "fsGroup"))
		}
	}

	for i, gid := range sc.SupplementalGroups {
		if gid < minGroupID || gid > maxGroupID {
			err := apis.ErrOutOfBoundsValue(gid, minGroupID, maxGroupID, "").
				ViaFieldIndex("supplementalGroups", i)
			errs = errs.Also(err)
		}
	}

	return errs
}

// This is attached to contexts as they are passed down through a user container
// being validated.
type userContainer struct{}

// WithUserContainer notes on the context that further validation or defaulting
// is within the context of a user container in the revision.
func WithinUserContainer(ctx context.Context) context.Context {
	return context.WithValue(ctx, userContainer{}, struct{}{})
}

// This is attached to contexts as they are passed down through a sidecar container
// being validated.
type sidecarContainer struct{}

// WithinSidecatrContainer notes on the context that further validation or defaulting
// is within the context of a sidecar container in the revision.
func WithinSidecarContainer(ctx context.Context) context.Context {
	return context.WithValue(ctx, sidecarContainer{}, struct{}{})
}

// Check if we are in the context of a sidecar container in the revision.
func IsInSidecarContainer(ctx context.Context) bool {
	return ctx.Value(sidecarContainer{}) != nil
}
