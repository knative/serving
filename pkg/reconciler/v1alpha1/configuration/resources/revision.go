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

	"github.com/knative/pkg/kmeta"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/configuration/resources/names"
	corev1 "k8s.io/api/core/v1"
)

func MakeRevision(config *v1alpha1.Configuration) *v1alpha1.Revision {
	// Start from the ObjectMeta/Spec inlined in the Configuration resources.
	rev := &v1alpha1.Revision{
		ObjectMeta: config.Spec.RevisionTemplate.ObjectMeta,
		Spec:       config.Spec.RevisionTemplate.Spec,
	}
	// Populate the Namespace and Nme
	rev.Namespace = config.Namespace
	rev.Name = names.Revision(config)

	// Populate the Configuration label.
	if rev.Labels == nil {
		rev.Labels = make(map[string]string)
	}
	rev.Labels[serving.ConfigurationLabelKey] = config.Name

	configLabels := config.Labels
	rev.Labels[serving.ServiceLabelKey] = configLabels[serving.ServiceLabelKey]
	rev.Labels[serving.ConfigurationGenerationLabelKey] = fmt.Sprintf("%v", config.Spec.Generation)

	// Populate the Configuration Generation annotation.
	if rev.Annotations == nil {
		rev.Annotations = make(map[string]string)
	}

	// Populate OwnerReferences so that deletes cascade.
	rev.OwnerReferences = append(rev.OwnerReferences, *kmeta.NewControllerRef(config))

	// Fill in the build name, if specified.
	rev.Spec.BuildName = names.Build(config)

	// Fill in buildRef if build is involved
	rev.Spec.BuildRef = buildRef(config)

	return rev
}

func buildRef(config *v1alpha1.Configuration) *corev1.ObjectReference {
	if config.Spec.Build == nil {
		return nil
	}

	b := MakeBuild(config)
	return &corev1.ObjectReference{
		APIVersion: b.GetAPIVersion(),
		Kind:       b.GetKind(),
		Name:       b.GetName(),
	}
}
