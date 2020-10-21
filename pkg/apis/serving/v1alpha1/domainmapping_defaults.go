package v1alpha1

import (
	"context"
)

// SetDefaults implements apis.Defaultable.
func (dm *DomainMapping) SetDefaults(ctx context.Context) {
	if dm.Spec.Ref.Namespace == "" {
		dm.Spec.Ref.Namespace = dm.Namespace
	}
}
