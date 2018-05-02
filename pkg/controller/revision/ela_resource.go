package revision

import (
	"github.com/elafros/elafros/pkg/apis/ela"
	"github.com/elafros/elafros/pkg/apis/ela/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const appLabelKey = "app"

// MakeElaResourceLabels constructs the labels we will apply to K8s resources.
func MakeElaResourceLabels(revision *v1alpha1.Revision) map[string]string {
	labels := make(map[string]string, len(revision.ObjectMeta.Labels)+2)
	labels[ela.RevisionLabelKey] = revision.Name

	for k, v := range revision.ObjectMeta.Labels {
		labels[k] = v
	}
	// If users don't specify an app: label we will automatically
	// populate it with the revision name to get the benefit of richer
	// tracing information.
	if _, ok := labels[appLabelKey]; !ok {
		labels[appLabelKey] = revision.Name
	}
	return labels
}

// MakeElaResourceSelector constructs the Selector we will apply to K8s resources.
func MakeElaResourceSelector(revision *v1alpha1.Revision) *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels:MakeElaResourceLabels(revision)}
}

// MakeElaResourceAnnotations creates the annotations we will apply to
// child resource of the given revision.
func MakeElaResourceAnnotations(revision *v1alpha1.Revision) map[string]string {
	annotations := make(map[string]string, len(revision.ObjectMeta.Annotations)+1)
	for k, v := range revision.ObjectMeta.Annotations {
		annotations[k] = v
	}
	return annotations
}
