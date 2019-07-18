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

	"knative.dev/pkg/kmeta"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
)

// MakeRevision creates a revision object from configuration.
func MakeRevision(config *v1alpha1.Configuration) *v1alpha1.Revision {
	// Start from the ObjectMeta/Spec inlined in the Configuration resources.
	rev := &v1alpha1.Revision{
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
func UpdateRevisionLabels(rev *v1alpha1.Revision, config *v1alpha1.Configuration) {
	if rev.Labels == nil {
		rev.Labels = make(map[string]string)
	}

	for _, key := range []string{
		serving.ConfigurationLabelKey,
		serving.ServiceLabelKey,
		serving.ConfigurationGenerationLabelKey,
	} {
		rev.Labels[key] = RevisionLabelValueForKey(key, config)
	}
}

// UpdateRevisionAnnotations sets the revisions annotations given a Configuration's updater annotation.
func UpdateRevisionAnnotations(rev *v1alpha1.Revision, config *v1alpha1.Configuration) {
	if rev.Annotations == nil {
		rev.Annotations = make(map[string]string)
	}

	// Populate the CreatorAnnotation from configuration.
	cans := config.GetAnnotations()
	if c, ok := cans[serving.UpdaterAnnotation]; ok {
		rev.Annotations[serving.CreatorAnnotation] = c
	}
}

// RevisionLabelValueForKey returns the label value for the given key.
func RevisionLabelValueForKey(key string, config *v1alpha1.Configuration) string {
	switch key {
	case serving.ConfigurationLabelKey:
		return config.Name
	case serving.ServiceLabelKey:
		return config.Labels[serving.ServiceLabelKey]
	case serving.ConfigurationGenerationLabelKey:
		return fmt.Sprintf("%d", config.Generation)
	}

	return ""
}
