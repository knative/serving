package revision

import "github.com/elafros/elafros/pkg/apis/ela/v1alpha1"

// MakeElaResourceLabels constructs the labels we will apply to K8s resources.
func MakeElaResourceLabels(u *v1alpha1.Revision) map[string]string {
	labels := map[string]string{
		elaVersionLabel: u.Name,
	}
	// Inherit route label from the revision if present.
	if route, ok := u.ObjectMeta.Labels[routeLabel]; ok {
		labels[routeLabel] = route
	}
	return labels
}
