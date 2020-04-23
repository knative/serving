/*
Copyright 2020 The Knative Authors.

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

package webhook

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"knative.dev/pkg/logging"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

var (
	// PodSpecDryRunAnnotation gates the podspec dryrun feature and runs with the value 'enabled'
	PodSpecDryRunAnnotation = "features.knative.dev/podspec-dryrun"
)

// ValidateRevisionTemplate runs extra validation on Service resources
func ValidateRevisionTemplate(ctx context.Context, uns *unstructured.Unstructured) error {
	content := uns.UnstructuredContent()

	// TODO(https://github.com/knative/serving/issues/3425): remove this guard once variations
	// of this are well-tested. Only run extra validation for the dry-run test.
	// This will be in place to while the feature is tested for compatibility and later removed.
	if uns.GetAnnotations()[PodSpecDryRunAnnotation] != "enabled" {
		return nil
	}

	namespace := uns.GetNamespace()

	// Decode and validate the RevisionTemplateSpec
	val, found, err := unstructured.NestedFieldNoCopy(content, "spec", "template")
	if err != nil {
		return fmt.Errorf("could not traverse nested spec.template field: %w", err)
	}
	if !found {
		logger := logging.FromContext(ctx)
		logger.Warn("no spec.template found for unstructured")
		return nil
	}

	templ, err := decodeTemplate(ctx, val)
	if err != nil {
		return err
	}
	if templ == nil || templ == (&v1.RevisionTemplateSpec{}) {
		return nil // Don't need to validate empty templates
	}
	if err := validatePodSpec(ctx, templ.Spec, namespace); err != nil {
		return err
	}
	return nil
}
