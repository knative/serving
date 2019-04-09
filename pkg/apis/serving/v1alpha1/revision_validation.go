/*
Copyright 2018 The Knative Authors

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

package v1alpha1

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/knative/pkg/apis"
	"github.com/knative/pkg/kmp"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/serving"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	minUserID = 0
	maxUserID = math.MaxInt32
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

func (current *Revision) checkImmutableFields(ctx context.Context, original *Revision) *apis.FieldError {
	if diff, err := kmp.ShortDiff(original.Spec, current.Spec); err != nil {
		return &apis.FieldError{
			Message: "Failed to diff Revision",
			Paths:   []string{"spec"},
			Details: err.Error(),
		}
	} else if diff != "" {
		return &apis.FieldError{
			Message: "Immutable fields changed (-old +new)",
			Paths:   []string{"spec"},
			Details: diff,
		}
	}
	return nil
}

// Validate ensures Revision is properly configured.
func (rt *Revision) Validate(ctx context.Context) *apis.FieldError {
	errs := serving.ValidateObjectMetadata(rt.GetObjectMeta()).ViaField("metadata")
	errs = errs.Also(rt.Spec.Validate(apis.WithinSpec(ctx)).ViaField("spec"))
	if apis.IsInUpdate(ctx) {
		old := apis.GetBaseline(ctx).(*Revision)
		errs = errs.Also(rt.checkImmutableFields(ctx, old))
	}
	return errs
}

// Validate ensures RevisionTemplateSpec is properly configured.
func (rt *RevisionTemplateSpec) Validate(ctx context.Context) *apis.FieldError {
	errs := rt.Spec.Validate(ctx).ViaField("spec")

	// If the RevisionTemplate has a name specified, then check that
	// it follows the requirements on the name.
	if rt.Name != "" {
		om := apis.ParentMeta(ctx)
		prefix := om.Name + "-"
		if om.Name != "" {
			// Even if there is GenerateName, allow the use
			// of Name post-creation.
		} else if om.GenerateName != "" {
			// We disallow bringing your own name when the parent
			// resource uses generateName (at creation).
			return apis.ErrDisallowedFields("metadata.name")
		}

		if !strings.HasPrefix(rt.Name, prefix) {
			errs = errs.Also(apis.ErrInvalidValue(
				fmt.Sprintf("%q must have prefix %q", rt.Name, prefix),
				"metadata.name"))
		}
	}

	return errs
}

// VerifyNameChange checks that if a user brought their own name previously that it
// changes at the appropriate times.
func (current *RevisionTemplateSpec) VerifyNameChange(ctx context.Context, og *RevisionTemplateSpec) *apis.FieldError {
	if current.Name == "" {
		// We only check that Name changes when the RevisionTemplate changes.
		return nil
	}
	if current.Name != og.Name {
		// The name changed, so we're good.
		return nil
	}

	if diff, err := kmp.ShortDiff(og, current); err != nil {
		return &apis.FieldError{
			Message: "Failed to diff RevisionTemplate",
			Paths:   []string{apis.CurrentField},
			Details: err.Error(),
		}
	} else if diff != "" {
		return &apis.FieldError{
			Message: "Saw the following changes without a name change (-old +new)",
			Paths:   []string{apis.CurrentField},
			Details: diff,
		}
	}
	return nil
}

// Validate ensures RevisionSpec is properly configured.
func (rs *RevisionSpec) Validate(ctx context.Context) *apis.FieldError {
	if equality.Semantic.DeepEqual(rs, &RevisionSpec{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}

	errs := CheckDeprecated(ctx, map[string]interface{}{
		"generation":       rs.DeprecatedGeneration,
		"servingState":     rs.DeprecatedServingState,
		"concurrencyModel": rs.DeprecatedConcurrencyModel,
		"buildName":        rs.DeprecatedBuildName,
	})

	volumes := sets.NewString()
	for i, volume := range rs.Volumes {
		if volumes.Has(volume.Name) {
			errs = errs.Also((&apis.FieldError{
				Message: fmt.Sprintf("duplicate volume name %q", volume.Name),
				Paths:   []string{"name"},
			}).ViaFieldIndex("volumes", i))
		}
		errs = errs.Also(validateVolume(volume).ViaFieldIndex("volumes", i))
		volumes.Insert(volume.Name)
	}

	if rs.Container != nil {
		errs = errs.Also(validateContainer(*rs.Container, volumes).
			ViaField("container"))
	} else {
		return apis.ErrMissingField("container")
	}
	errs = errs.Also(validateBuildRef(rs.BuildRef).ViaField("buildRef"))

	if err := rs.DeprecatedConcurrencyModel.Validate(ctx).ViaField("concurrencyModel"); err != nil {
		errs = errs.Also(err)
	} else {
		errs = errs.Also(ValidateContainerConcurrency(
			rs.ContainerConcurrency, rs.DeprecatedConcurrencyModel))
	}

	if rs.TimeoutSeconds != nil {
		errs = errs.Also(validateTimeoutSeconds(*rs.TimeoutSeconds))
	}
	return errs
}

func validateTimeoutSeconds(timeoutSeconds int64) *apis.FieldError {
	if timeoutSeconds != 0 {
		if timeoutSeconds > int64(networking.DefaultTimeout.Seconds()) || timeoutSeconds < 0 {
			return apis.ErrOutOfBoundsValue(timeoutSeconds, 0,
				networking.DefaultTimeout.Seconds(),
				"timeoutSeconds")
		}
	}
	return nil
}

// Validate ensures DeprecatedRevisionServingStateType is properly configured.
func (ss DeprecatedRevisionServingStateType) Validate(ctx context.Context) *apis.FieldError {
	switch ss {
	case DeprecatedRevisionServingStateType(""),
		DeprecatedRevisionServingStateRetired,
		DeprecatedRevisionServingStateReserve,
		DeprecatedRevisionServingStateActive:
		return nil
	default:
		return apis.ErrInvalidValue(ss, apis.CurrentField)
	}
}

// Validate ensures RevisionRequestConcurrencyModelType is properly configured.
func (cm RevisionRequestConcurrencyModelType) Validate(ctx context.Context) *apis.FieldError {
	switch cm {
	case RevisionRequestConcurrencyModelType(""),
		RevisionRequestConcurrencyModelMulti,
		RevisionRequestConcurrencyModelSingle:
		return nil
	default:
		return apis.ErrInvalidValue(cm, apis.CurrentField)
	}
}

// ValidateContainerConcurrency ensures ContainerConcurrency is properly configured.
func ValidateContainerConcurrency(cc RevisionContainerConcurrencyType, cm RevisionRequestConcurrencyModelType) *apis.FieldError {
	// Validate ContainerConcurrency alone
	if cc < 0 || cc > RevisionContainerConcurrencyMax {
		return apis.ErrInvalidValue(cc, "containerConcurrency")
	}

	// Validate combinations of ConcurrencyModel and ContainerConcurrency

	if cc == 0 && cm != RevisionRequestConcurrencyModelMulti && cm != RevisionRequestConcurrencyModelType("") {
		return apis.ErrMultipleOneOf("containerConcurrency", "concurrencyModel")
	}
	if cc == 1 && cm != RevisionRequestConcurrencyModelSingle && cm != RevisionRequestConcurrencyModelType("") {
		return apis.ErrMultipleOneOf("containerConcurrency", "concurrencyModel")
	}
	if cc > 1 && cm != RevisionRequestConcurrencyModelType("") {
		return apis.ErrMultipleOneOf("containerConcurrency", "concurrencyModel")
	}

	return nil
}

func validateVolume(volume corev1.Volume) *apis.FieldError {
	errs := validateDisallowedFields(volume, *VolumeMask(&volume))
	if volume.Name == "" {
		errs = apis.ErrMissingField("name")
	} else if len(validation.IsDNS1123Label(volume.Name)) != 0 {
		errs = apis.ErrInvalidValue(volume.Name, "name")
	}

	vs := volume.VolumeSource
	errs = errs.Also(validateDisallowedFields(vs, *VolumeSourceMask(&vs)))
	if vs.Secret == nil && vs.ConfigMap == nil {
		errs = errs.Also(apis.ErrMissingOneOf("secret", "configMap"))
	}
	return errs
}

func validateEnvValueFrom(source *corev1.EnvVarSource) *apis.FieldError {
	if source == nil {
		return nil
	}
	return validateDisallowedFields(*source, *EnvVarSourceMask(source))
}

func validateEnv(envVars []corev1.EnvVar) *apis.FieldError {
	var errs *apis.FieldError
	for i, env := range envVars {
		errs = errs.Also(validateDisallowedFields(env, *EnvVarMask(&env)).ViaIndex(i)).Also(
			validateEnvValueFrom(env.ValueFrom).ViaField("valueFrom").ViaIndex(i))
	}
	return errs
}

func validateEnvFrom(envFromList []corev1.EnvFromSource) *apis.FieldError {
	var errs *apis.FieldError
	for i, envFrom := range envFromList {
		errs = errs.Also(validateDisallowedFields(envFrom, *EnvFromSourceMask(&envFrom)).ViaIndex(i))

		cm := envFrom.ConfigMapRef
		sm := envFrom.SecretRef
		if sm != nil {
			errs = errs.Also(validateDisallowedFields(*sm, *SecretEnvSourceMask(sm)).ViaIndex(i))
			errs = errs.Also(validateDisallowedFields(
				sm.LocalObjectReference, *LocalObjectReferenceMask(&sm.LocalObjectReference))).ViaIndex(i).ViaField("secretRef")
		}

		if cm != nil {
			errs = errs.Also(validateDisallowedFields(*cm, *ConfigMapEnvSourceMask(cm)).ViaIndex(i))
			errs = errs.Also(validateDisallowedFields(
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

func validateContainer(container corev1.Container, volumes sets.String) *apis.FieldError {
	if equality.Semantic.DeepEqual(container, corev1.Container{}) {
		return apis.ErrMissingField(apis.CurrentField)
	}

	errs := validateDisallowedFields(container, *ContainerMask(&container))

	// Env
	errs = errs.Also(validateEnv(container.Env).ViaField("env"))
	// EnvFrom
	errs = errs.Also(validateEnvFrom(container.EnvFrom).ViaField("envFrom"))
	// Image
	if _, err := name.ParseReference(container.Image, name.WeakValidation); err != nil {
		fe := &apis.FieldError{
			Message: "Failed to parse image reference",
			Paths:   []string{"image"},
			Details: fmt.Sprintf("image: %q, error: %v", container.Image, err),
		}
		errs = errs.Also(fe)
	}
	// Liveness Probes
	errs = errs.Also(validateProbe(container.LivenessProbe).ViaField("livenessProbe"))
	// Ports
	errs = errs.Also(validateContainerPorts(container.Ports).ViaField("ports"))
	// Readiness Probes
	errs = errs.Also(validateProbe(container.ReadinessProbe).ViaField("readinessProbe"))
	// Resources
	errs = errs.Also(validateResources(&container.Resources).ViaField("resources"))
	// SecurityContext
	errs = errs.Also(validateSecurityContext(container.SecurityContext).ViaField("securityContext"))
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
	return validateDisallowedFields(*resources, *ResourceRequirementsMask(resources))
}

func validateSecurityContext(sc *corev1.SecurityContext) *apis.FieldError {
	if sc == nil {
		return nil
	}
	errs := validateDisallowedFields(*sc, *SecurityContextMask(sc))

	if sc.RunAsUser != nil {
		uid := *sc.RunAsUser
		if uid < minUserID || uid > maxUserID {
			errs = errs.Also(apis.ErrOutOfBoundsValue(uid, minUserID, maxUserID, "runAsUser"))
		}
	}
	return errs
}

func validateVolumeMounts(mounts []corev1.VolumeMount, volumes sets.String) *apis.FieldError {
	var errs *apis.FieldError
	// Check that volume mounts match names in "volumes", that "volumes" has 100%
	// coverage, and the field restrictions.
	seen := sets.NewString()
	for i, vm := range mounts {

		errs = errs.Also(validateDisallowedFields(vm, *VolumeMountMask(&vm)).ViaIndex(i))
		// This effectively checks that Name is non-empty because Volume name must be non-empty.
		if !volumes.Has(vm.Name) {
			errs = errs.Also((&apis.FieldError{
				Message: "volumeMount has no matching volume",
				Paths:   []string{"name"},
			}).ViaIndex(i))
		}
		seen.Insert(vm.Name)

		if vm.MountPath == "" {
			errs = errs.Also(apis.ErrMissingField("mountPath").ViaIndex(i))
		} else if reservedPaths.Has(filepath.Clean(vm.MountPath)) {
			errs = errs.Also((&apis.FieldError{
				Message: fmt.Sprintf("mountPath %q is a reserved path", filepath.Clean(vm.MountPath)),
				Paths:   []string{"mountPath"},
			}).ViaIndex(i))
		} else if !filepath.IsAbs(vm.MountPath) {
			errs = errs.Also(apis.ErrInvalidValue(vm.MountPath, "mountPath").ViaIndex(i))
		}
		if !vm.ReadOnly {
			errs = errs.Also(apis.ErrMissingField("readOnly").ViaIndex(i))
		}

	}

	if missing := volumes.Difference(seen); missing.Len() > 0 {
		errs = errs.Also(&apis.FieldError{
			Message: fmt.Sprintf("volumes not mounted: %v", missing.List()),
			Paths:   []string{""},
		})
	}
	return errs
}

func validateDisallowedFields(request, maskedRequest interface{}) *apis.FieldError {
	if disallowed, err := kmp.CompareSetFields(request, maskedRequest); err != nil {
		return &apis.FieldError{
			Message: fmt.Sprintf("Internal Error: %v", err),
		}
	} else if len(disallowed) > 0 {
		return apis.ErrDisallowedFields(disallowed...)
	}
	return nil
}

func validateContainerPorts(ports []corev1.ContainerPort) *apis.FieldError {
	if len(ports) == 0 {
		return nil
	}

	var errs *apis.FieldError

	// user can set container port which names "user-port" to define application's port.
	// Queue-proxy will use it to send requests to application
	// if user didn't set any port, it will set default port user-port=8080.
	if len(ports) > 1 {
		errs = errs.Also(&apis.FieldError{
			Message: "More than one container port is set",
			Paths:   []string{apis.CurrentField},
			Details: "Only a single port is allowed",
		})
	}

	userPort := ports[0]

	errs = errs.Also(validateDisallowedFields(userPort, *ContainerPortMask(&userPort)))

	// Only allow empty (defaulting to "TCP") or explicit TCP for protocol
	if userPort.Protocol != "" && userPort.Protocol != corev1.ProtocolTCP {
		errs = errs.Also(apis.ErrInvalidValue(userPort.Protocol, "protocol"))
	}

	// Don't allow userPort to conflict with QueueProxy sidecar
	if userPort.ContainerPort == RequestQueuePort ||
		userPort.ContainerPort == RequestQueueAdminPort ||
		userPort.ContainerPort == RequestQueueMetricsPort {
		errs = errs.Also(apis.ErrInvalidValue(userPort.ContainerPort, "containerPort"))
	}

	if userPort.ContainerPort < 1 || userPort.ContainerPort > 65535 {
		errs = errs.Also(apis.ErrOutOfBoundsValue(userPort.ContainerPort,
			1, 65535, "containerPort"))
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

func validateBuildRef(buildRef *corev1.ObjectReference) *apis.FieldError {
	if buildRef == nil {
		return nil
	}
	if len(validation.IsQualifiedName(buildRef.APIVersion)) != 0 {
		return apis.ErrInvalidValue(buildRef.APIVersion, "apiVersion")
	}
	if len(validation.IsCIdentifier(buildRef.Kind)) != 0 {
		return apis.ErrInvalidValue(buildRef.Kind, "kind")
	}
	if len(validation.IsDNS1123Label(buildRef.Name)) != 0 {
		return apis.ErrInvalidValue(buildRef.Name, "name")
	}
	var disallowedFields []string
	if buildRef.Namespace != "" {
		disallowedFields = append(disallowedFields, "namespace")
	}
	if buildRef.FieldPath != "" {
		disallowedFields = append(disallowedFields, "fieldPath")
	}
	if buildRef.ResourceVersion != "" {
		disallowedFields = append(disallowedFields, "resourceVersion")
	}
	if buildRef.UID != "" {
		disallowedFields = append(disallowedFields, "uid")
	}
	if len(disallowedFields) != 0 {
		return apis.ErrDisallowedFields(disallowedFields...)
	}
	return nil
}

func validateProbe(p *corev1.Probe) *apis.FieldError {
	if p == nil {
		return nil
	}
	emptyPort := intstr.IntOrString{}
	switch {
	case p.Handler.HTTPGet != nil:
		if p.Handler.HTTPGet.Port != emptyPort {
			return apis.ErrDisallowedFields("httpGet.port")
		}
	case p.Handler.TCPSocket != nil:
		if p.Handler.TCPSocket.Port != emptyPort {
			return apis.ErrDisallowedFields("tcpSocket.port")
		}
	}
	return nil
}
