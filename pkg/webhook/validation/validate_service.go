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

package validation

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/logging"
)

// ExtraServiceValidation runs extra validation on Service resources
func ExtraServiceValidation(ctx context.Context, uns *unstructured.Unstructured) error {
	// TODO(whaught): remove this guard once variations of this are well-tested
	// Only run extra validation if we are in dry-run mode. This will be in place to while
	// the feature is tested for compatability and later removed.
	if !apis.IsDryRun(ctx) {
		return nil
	}

	content := uns.UnstructuredContent()
	namespace, _, err := unstructured.NestedString(content, "metadata", "namespace")
	if err != nil {
		return fmt.Errorf("could not traverse nested objectmeta.namespace field: %w", err)
	}

	// Decode and validate the RevisionTemplateSpec
	val, found, err := unstructured.NestedFieldNoCopy(content, "spec", "template")
	if err != nil {
		return fmt.Errorf("could not traverse nested spec.template field: %w", err)
	}
	if !found {
		logger := logging.FromContext(ctx)
		logger.Warnw("no spec.template found for unstructured")
		return nil
	}

	return decodeTemplateAndValidate(ctx, val, namespace)
}
