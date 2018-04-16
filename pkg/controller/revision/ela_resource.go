package revision

import (
	"github.com/elafros/elafros/pkg/apis/ela"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
)

const appLabelKey = "app"

// MakeElaResourceLabels constructs the labels we will apply to K8s resources.
func MakeElaResourceLabels(revision *v1alpha1.Revision) map[string]string {
	labels := make(map[string]string, len(revision.ObjectMeta.Labels)+1)
	labels[ela.RevisionLabelKey] = revision.Name

	hasAppLabel := false
	for k, v := range revision.ObjectMeta.Labels {
		labels[k] = v
		if k == appLabelKey {
			hasAppLabel = true
		}
	}
	// If users don't specify an app: label we will automatically
	// populate it with the revision name to get the benefit of richer
	// tracing information.
	if !hasAppLabel {
		labels[appLabelKey] = revision.Name
	}
	return labels
}

func MakeElaResourceAnnotations(revision *v1alpha1.Revision) map[string]string {
	annotations := make(map[string]string, len(revision.ObjectMeta.Annotations)+1)
	for k, v := range revision.ObjectMeta.Annotations {
		annotations[k] = v
	}
	return annotations
}
