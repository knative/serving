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

package resources

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

// MakeRevision creates a revision object from configuration.
func MakeRevision(config *v1.Configuration) *v1.Revision {
	// Start from the ObjectMeta/Spec inlined in the Configuration resources.
	rev := &v1.Revision{
		ObjectMeta: config.Spec.GetTemplate().ObjectMeta,
		Spec:       config.Spec.GetTemplate().Spec,
	}
	// Populate the Namespace and Name.
	rev.Namespace = config.Namespace

	if rev.Name == "" {
		rev.GenerateName = config.Name + "-"
	}

	UpdateRevisionLabels(rev, config)
	UpdateRevisionAnnotations(rev, config)

	// Populate OwnerReferences so that deletes cascade.
	rev.OwnerReferences = append(rev.OwnerReferences, *kmeta.NewControllerRef(config))

	return rev
}

// UpdateRevisionLabels sets the revisions labels given a Configuration.
func UpdateRevisionLabels(rev, config metav1.Object) {
	labels := rev.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	for _, key := range []string{
		serving.ConfigurationLabelKey,
		serving.ServiceLabelKey,
		serving.ConfigurationGenerationLabelKey,
	} {
		labels[key] = RevisionLabelValueForKey(key, config)
	}

	rev.SetLabels(labels)
}

// UpdateRevisionAnnotations sets the revisions annotations given a Configuration's updater annotation.
func UpdateRevisionAnnotations(rev, config metav1.Object) {
	annotations := rev.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// Populate the CreatorAnnotation from configuration.
	cans := config.GetAnnotations()
	if c, ok := cans[serving.UpdaterAnnotation]; ok {
		annotations[serving.CreatorAnnotation] = c
	}

	rev.SetAnnotations(annotations)
}

// RevisionLabelValueForKey returns the label value for the given key.
func RevisionLabelValueForKey(key string, config metav1.Object) string {
	switch key {
	case serving.ConfigurationLabelKey:
		return config.GetName()
	case serving.ServiceLabelKey:
		return config.GetLabels()[serving.ServiceLabelKey]
	case serving.ConfigurationGenerationLabelKey:
		return fmt.Sprintf("%d", config.GetGeneration())
	}

	return ""
}
