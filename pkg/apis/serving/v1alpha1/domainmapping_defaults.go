package v1alpha1

import (
	"context"
)

// SetDefaults implements apis.Defaultable.
func (r *DomainMapping) SetDefaults(ctx context.Context) {
	if r.Spec.Ref.Namespace == "" {
		r.Spec.Ref.Namespace = r.Namespace
	}
}
