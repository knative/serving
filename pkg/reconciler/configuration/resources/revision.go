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
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	"knative.dev/pkg/kmeta"
	cfgMap "knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

// MakeRevision creates a revision object from configuration.
func MakeRevision(ctx context.Context, config *v1.Configuration, clock clock.Clock) *v1.Revision {
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

	// Pending tells the labeler that we have not processed this revision.
	if cfgMap.FromContextOrDefaults(ctx).Features.ResponsiveRevisionGC != cfgMap.Disabled {
		rev.SetRoutingState(v1.RoutingStatePending, clock)
	}

	updateRevisionLabels(rev, config)
	updateRevisionAnnotations(rev, config)

	// Populate OwnerReferences so that deletes cascade.
	rev.OwnerReferences = append(rev.OwnerReferences, *kmeta.NewControllerRef(config))

	return rev
}

// updateRevisionLabels sets the revisions labels given a Configuration.
func updateRevisionLabels(rev, config metav1.Object) {
	labels := rev.GetLabels()
	if labels == nil {
		labels = make(map[string]string, 3)
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

// updateRevisionAnnotations sets the revision's annotation given a Configuration's updater annotation.
func updateRevisionAnnotations(rev *v1.Revision, config metav1.Object) {
	annotations := rev.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string, 1)
	}

	// Populate the CreatorAnnotation from configuration's Updater annotation.
	cans := config.GetAnnotations()
	if c, ok := cans[serving.UpdaterAnnotation]; ok {
		annotations[serving.CreatorAnnotation] = c
	}

	if v, ok := cans[serving.RoutesAnnotationKey]; ok {
		annotations[serving.RoutesAnnotationKey] = v
		rev.SetRoutingState(v1.RoutingStateActive, clock.RealClock{})
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
		return fmt.Sprint(config.GetGeneration())
	}
	return ""
}
