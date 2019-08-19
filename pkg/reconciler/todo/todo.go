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

package todo

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/serving/pkg/apis/internalversions/serving"
	servingcommon "knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	listers "knative.dev/serving/pkg/client/serving/listers/serving/internalversion"
)

// RevisionLabelValueForKey returns the label value for the given key.
func RevisionLabelValueForKey(key string, config *serving.Configuration) string {
	switch key {
	case servingcommon.ConfigurationLabelKey:
		return config.Name
	case servingcommon.ServiceLabelKey:
		return config.Labels[servingcommon.ServiceLabelKey]
	case servingcommon.ConfigurationGenerationLabelKey:
		return fmt.Sprintf("%d", config.Generation)
	}

	return ""
}

// CheckNameAvailability checks that if the named Revision specified by the Configuration
// is available (not found), exists (but matches), or exists with conflict (doesn't match).
func CheckNameAvailability(config *serving.Configuration, lister listers.RevisionLister) (*serving.Revision, error) {
	// If config.Spec.GetTemplate().Name is set, then we can directly look up
	// the revision by name.
	name := config.Spec.GetTemplate().Name
	if name == "" {
		return nil, nil
	}
	errConflict := errors.NewAlreadyExists(v1alpha1.Resource("revisions"), name)

	rev, err := lister.Revisions(config.Namespace).Get(name)
	if errors.IsNotFound(err) {
		// Does not exist, we must be good!
		// note: for the name to change the generation must change.
		return nil, err
	} else if err != nil {
		return nil, err
	} else if !metav1.IsControlledBy(rev, config) {
		// If the revision isn't controller by this configuration, then
		// do not use it.
		return nil, errConflict
	}

	// Check the generation on this revision.
	generationKey := servingcommon.ConfigurationGenerationLabelKey
	expectedValue := RevisionLabelValueForKey(generationKey, config)
	if rev.Labels != nil && rev.Labels[generationKey] == expectedValue {
		return rev, nil
	}
	// We only require spec equality because the rest is immutable and the user may have
	// annotated or labeled the Revision (beyond what the Configuration might have).
	if !equality.Semantic.DeepEqual(config.Spec.GetTemplate().Spec, rev.Spec) {
		return nil, errConflict
	}
	return rev, nil
}
