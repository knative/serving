package v1alpha1

import "github.com/knative/pkg/apis"

// Validate ClusterBuildTemplate
func (b *ClusterBuildTemplate) Validate() *apis.FieldError {
	return validateObjectMetadata(b.GetObjectMeta()).ViaField("metadata").Also(b.Spec.Validate().ViaField("spec"))
}
