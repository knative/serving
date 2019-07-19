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

	"knative.dev/pkg/apis"
	"knative.dev/serving/pkg/apis/serving"
	"knative.dev/serving/pkg/reconciler/route/config"
)

// Validate makes sure that Configuration is properly configured.
func (c *Configuration) Validate(ctx context.Context) (errs *apis.FieldError) {
	errs = errs.Also(c.ValidateLabels())
	// If we are in a status sub resource update, the metadata and spec cannot change.
	// So, to avoid rejecting controller status updates due to validations that may
	// have changed (i.e. due to config-defaults changes), we elide the metadata and
	// spec validation.
	if !apis.IsInStatusUpdate(ctx) {
		errs = errs.Also(serving.ValidateObjectMetadata(c.GetObjectMeta()).ViaField("metadata"))
		ctx = apis.WithinParent(ctx, c.ObjectMeta)
		errs = errs.Also(c.Spec.Validate(apis.WithinSpec(ctx)).ViaField("spec"))
	}

	errs = errs.Also(c.Status.Validate(apis.WithinStatus(ctx)).ViaField("status"))

	if apis.IsInUpdate(ctx) {
		original := apis.GetBaseline(ctx).(*Configuration)

		err := c.Spec.Template.VerifyNameChange(ctx, original.Spec.Template)
		errs = errs.Also(err.ViaField("spec.template"))
	}

	return errs
}

// Validate implements apis.Validatable
func (cs *ConfigurationSpec) Validate(ctx context.Context) *apis.FieldError {
	return cs.Template.Validate(ctx).ViaField("template")
}

// Validate implements apis.Validatable
func (cs *ConfigurationStatus) Validate(ctx context.Context) *apis.FieldError {
	return cs.ConfigurationStatusFields.Validate(ctx)
}

// Validate implements apis.Validatable
func (csf *ConfigurationStatusFields) Validate(ctx context.Context) *apis.FieldError {
	return nil
}

// ValidateLabels function validates service labels
func (c *Configuration) ValidateLabels() *apis.FieldError {
	var errs *apis.FieldError
LabelLoop:
	for key, val := range c.GetLabels() {
		switch {
		case key == config.VisibilityLabelKey:
			errs = errs.Also(validateClusterVisbilityLabel(val))
		case key == serving.RouteLabelKey:
		case key == serving.ServiceLabelKey:
			for _, ref := range c.GetOwnerReferences() {
				if ref.Kind == "Service" && val == ref.Name {
					continue LabelLoop
				}
			}
			errs = errs.Also(apis.ErrInvalidValue(val, key).ViaField("labels"))
		case strings.HasPrefix(key, serving.GroupName+"/"):
			errs = errs.Also(apis.ErrInvalidKeyName(key, "labels"))
		}
	}
	return errs.ViaField("metadata")
}
