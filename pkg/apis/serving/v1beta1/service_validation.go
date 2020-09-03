/*
Copyright 2019 The Knative Authors

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

package v1beta1

import (
	"context"
	"strings"

	network "knative.dev/networking/pkg"
	"knative.dev/pkg/apis"
	"knative.dev/serving/pkg/apis/serving"
)

// Validate makes sure that Service is properly configured.
func (s *Service) Validate(ctx context.Context) (errs *apis.FieldError) {
	// If we are in a status sub resource update, the metadata and spec cannot change.
	// So, to avoid rejecting controller status updates due to validations that may
	// have changed (i.e. due to config-defaults changes), we elide the metadata and
	// spec validation.
	if !apis.IsInStatusUpdate(ctx) {
		errs = errs.Also(serving.ValidateObjectMetadata(ctx, s.GetObjectMeta()).Also(
			s.validateLabels().ViaField("labels")).ViaField("metadata"))
		ctx = apis.WithinParent(ctx, s.ObjectMeta)
		errs = errs.Also(s.Spec.Validate(apis.WithinSpec(ctx)).ViaField("spec"))
	}

	errs = errs.Also(s.Status.Validate(apis.WithinStatus(ctx)).ViaField("status"))

	if apis.IsInUpdate(ctx) {
		original := apis.GetBaseline(ctx).(*Service)
		errs = errs.Also(apis.ValidateCreatorAndModifier(original.Spec, s.Spec, original.GetAnnotations(),
			s.GetAnnotations(), serving.GroupName).ViaField("metadata.annotations"))
		err := s.Spec.ConfigurationSpec.Template.VerifyNameChange(ctx,
			original.Spec.ConfigurationSpec.Template)
		errs = errs.Also(err.ViaField("spec.template"))
	}
	return errs
}

// validateLabels function validates service labels
func (s *Service) validateLabels() (errs *apis.FieldError) {
	for key, val := range s.GetLabels() {
		switch {
		case key == network.VisibilityLabelKey || key == serving.VisibilityLabelKeyObsolete:
			errs = errs.Also(serving.ValidateClusterVisibilityLabel(val, key))
		case strings.HasPrefix(key, serving.GroupNamePrefix):
			errs = errs.Also(apis.ErrInvalidKeyName(key, apis.CurrentField))
		}
	}
	return
}
