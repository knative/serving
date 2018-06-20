/*
Copyright 2018 Google LLC

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

package revision

import (
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const appLabelKey = "app"

// MakeServingResourceLabels constructs the labels we will apply to K8s resources.
func MakeServingResourceLabels(revision *v1alpha1.Revision) map[string]string {
	labels := make(map[string]string, len(revision.ObjectMeta.Labels)+2)
	labels[serving.RevisionLabelKey] = revision.Name
	labels[serving.RevisionUID] = string(revision.UID)

	for k, v := range revision.ObjectMeta.Labels {
		// TODO: Use a fixed set for revision labels. If the set of labels changed,
		// the deployment could end up with multiple replica sets.
		// https://github.com/knative/serving/issues/1293
		// The route for a revision could change, therefore it is excluded
		// in revision labels as a temporary solution.
		if k != serving.RouteLabelKey {
			labels[k] = v
		}
	}
	// If users don't specify an app: label we will automatically
	// populate it with the revision name to get the benefit of richer
	// tracing information.
	if _, ok := labels[appLabelKey]; !ok {
		labels[appLabelKey] = revision.Name
	}
	return labels
}

// MakeServingResourceSelector constructs the Selector we will apply to K8s resources.
func MakeServingResourceSelector(revision *v1alpha1.Revision) *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels: MakeServingResourceLabels(revision)}
}

// MakeServingResourceAnnotations creates the annotations we will apply to
// child resource of the given revision.
func MakeServingResourceAnnotations(revision *v1alpha1.Revision) map[string]string {
	annotations := make(map[string]string, len(revision.ObjectMeta.Annotations)+1)
	for k, v := range revision.ObjectMeta.Annotations {
		annotations[k] = v
	}
	return annotations
}
