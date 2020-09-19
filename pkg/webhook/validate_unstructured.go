/*
Copyright 2020 The Knative Authors

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

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/logging"
	"knative.dev/serving/pkg/apis/config"
	"knative.dev/serving/pkg/apis/serving"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
)

// PodSpecDryRunAnnotation gates the podspec dryrun feature and runs with the value 'enabled'
const PodSpecDryRunAnnotation = "features.knative.dev/podspec-dryrun"

// DryRunMode represents possible values of the PodSpecDryRunAnnotation
type DryRunMode string

const (
	// DryRunEnabled will run the dryrun logic. Will succeed if dryrun is unsupported.
	DryRunEnabled DryRunMode = "enabled"

	// DryRunStrict will run the dryrun logic and fail if dryrun is not supported.
	DryRunStrict DryRunMode = "strict"
)

// ValidateService runs extra validation on Service resources
func ValidateService(ctx context.Context, uns *unstructured.Unstructured) error {
	return validateRevisionTemplate(ctx, uns)
}

// ValidateConfiguration runs extra validation on Configuration resources
func ValidateConfiguration(ctx context.Context, uns *unstructured.Unstructured) error {
	// If owned by a service, skip validation for Configuration.
	if uns.GetLabels()[serving.ServiceLabelKey] != "" {
		return nil
	}

	return validateRevisionTemplate(ctx, uns)
}

func validateRevisionTemplate(ctx context.Context, uns *unstructured.Unstructured) error {
	content := uns.UnstructuredContent()

	mode := DryRunMode(uns.GetAnnotations()[PodSpecDryRunAnnotation])
	features := config.FromContextOrDefaults(ctx).Features
	switch features.PodSpecDryRun {
	case config.Enabled:
		if mode != DryRunStrict {
			mode = DryRunEnabled
		}
	case config.Disabled:
		return nil
	}

	// TODO(https://github.com/knative/serving/issues/3425): remove this guard once variations
	// of this are well-tested. Only run extra validation for the dry-run test.
	// This will be in place to while the feature is tested for compatibility and later removed.
	if mode != DryRunStrict && mode != DryRunEnabled {
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

	if apis.IsInUpdate(ctx) {
		if uns, err := runtime.DefaultUnstructuredConverter.ToUnstructured(apis.GetBaseline(ctx)); err == nil {
			if oldVal, found, _ := unstructured.NestedFieldNoCopy(uns, "spec", "template"); found &&
				equality.Semantic.DeepEqual(val, oldVal) {
				return nil // Don't validate no-change updates.
			}
		}
	}

	if err := validatePodSpec(ctx, templ.Spec, namespace, mode); err != nil {
		return err
	}
	return nil
}
