package v1alpha1

import (
	"fmt"
	"time"

	"github.com/knative/pkg/apis"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Validate Build
func (b *Build) Validate() *apis.FieldError {
	return validateObjectMetadata(b.GetObjectMeta()).ViaField("metadata").Also(b.Spec.Validate().ViaField("spec"))
}

// Validate for build spec
func (bs *BuildSpec) Validate() *apis.FieldError {
	if bs.Template == nil && len(bs.Steps) == 0 {
		return apis.ErrMissingField("b.spec.template").Also(apis.ErrMissingField("b.spec.steps"))
	}
	if bs.Template != nil && len(bs.Steps) > 0 {
		return apis.ErrMissingField("b.spec.template").Also(apis.ErrMissingField("b.spec.steps"))
	}

	if bs.Template != nil && bs.Template.Name == "" {
		apis.ErrMissingField("build.spec.template.name")
	}

	// If a build specifies a template, all the template's parameters without
	// defaults must be satisfied by the build's parameters.
	//var volumes []corev1.Volume
	//var tmpl BuildTemplateInterface
	if bs.Template != nil {
		return bs.Template.Validate()
	}
	if err := validateVolumes(bs.Volumes); err != nil {
		return err
	}
	if err := validateTimeout(bs.Timeout); err != nil {
		return err
	}
	if err := validateSteps(bs.Steps); err != nil {
		return err
	}
	return nil
}

// Validate templateKind
func (b *TemplateInstantiationSpec) Validate() *apis.FieldError {
	if b.Name == "" {
		return apis.ErrMissingField("build.spec.template.name")
	}
	if b.Kind != "" {
		switch b.Kind {
		case ClusterBuildTemplateKind,
			BuildTemplateKind:
			return nil
		default:
			return apis.ErrInvalidValue(string(b.Kind), apis.CurrentField)
		}
	}
	return nil
}

func validateTimeout(timeout metav1.Duration) *apis.FieldError {
	maxTimeout := time.Duration(24 * time.Hour)

	if timeout.Duration > maxTimeout {
		return apis.ErrInvalidValue(fmt.Sprintf("%s should be < 24h", timeout), "b.spec.timeout")
	} else if timeout.Duration < 0 {
		return apis.ErrInvalidValue(fmt.Sprintf("%s should be > 0", timeout), "b.spec.timeout")
	}
	return nil
}
