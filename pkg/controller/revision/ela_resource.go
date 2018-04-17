package revision

import (
	"github.com/elafros/elafros/pkg/apis/ela"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
)

// MakeElaResourceLabels constructs the labels we will apply to K8s resources.
func MakeElaResourceLabels(u *v1alpha1.Revision) map[string]string {
	labels := make(map[string]string, len(u.ObjectMeta.Labels)+1)
	labels[ela.RevisionLabelKey] = u.Name

	for k, v := range u.ObjectMeta.Labels {
		labels[k] = v
	}
	return labels
}

func MakeElaResourceAnnotations(u *v1alpha1.Revision) map[string]string {
	annotations := make(map[string]string, len(u.ObjectMeta.Annotations)+1)
	for k, v := range u.ObjectMeta.Annotations {
		annotations[k] = v
	}
	return annotations
}
