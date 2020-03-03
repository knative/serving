/*
Copyright 2019 The Knative Authors.

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

package v1alpha1

import (
	"context"

	"knative.dev/pkg/apis"
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	"knative.dev/serving/pkg/apis/serving/v1beta1"
)

// ConvertTo implements apis.Convertible
func (source *Configuration) ConvertTo(ctx context.Context, obj apis.Convertible) error {
	switch sink := obj.(type) {
	case *v1beta1.Configuration:
		sink.ObjectMeta = source.ObjectMeta
		if err := source.Spec.ConvertTo(ctx, &sink.Spec); err != nil {
			return err
		}
		return source.Status.ConvertTo(ctx, &sink.Status)
	default:
		return apis.ConvertToViaProxy(ctx, source, &v1beta1.Configuration{}, sink)
	}
}

// ConvertTo helps implement apis.Convertible
func (source *ConfigurationSpec) ConvertTo(ctx context.Context, sink *v1.ConfigurationSpec) error {
	if source.DeprecatedBuild != nil {
		return ConvertErrorf("build", "build cannot be migrated forward.")
	}
	switch {
	case source.DeprecatedRevisionTemplate != nil && source.Template != nil:
		return apis.ErrMultipleOneOf("revisionTemplate", "template")
	case source.DeprecatedRevisionTemplate != nil:
		return source.DeprecatedRevisionTemplate.ConvertTo(ctx, &sink.Template)
	case source.Template != nil:
		return source.Template.ConvertTo(ctx, &sink.Template)
	default:
		return apis.ErrMissingOneOf("revisionTemplate", "template")
	}
}

// ConvertTo helps implement apis.Convertible
func (source *ConfigurationStatus) ConvertTo(ctx context.Context, sink *v1.ConfigurationStatus) error {
	source.Status.ConvertTo(ctx, &sink.Status, v1.IsConfigurationCondition)
	return source.ConfigurationStatusFields.ConvertTo(ctx, &sink.ConfigurationStatusFields)
}

// ConvertTo helps implement apis.Convertible
func (source *ConfigurationStatusFields) ConvertTo(ctx context.Context, sink *v1.ConfigurationStatusFields) error {
	sink.LatestReadyRevisionName = source.LatestReadyRevisionName
	sink.LatestCreatedRevisionName = source.LatestCreatedRevisionName
	return nil
}

// ConvertFrom implements apis.Convertible
func (sink *Configuration) ConvertFrom(ctx context.Context, obj apis.Convertible) error {
	switch source := obj.(type) {
	case *v1beta1.Configuration:
		sink.ObjectMeta = source.ObjectMeta
		if err := sink.Spec.ConvertFrom(ctx, source.Spec); err != nil {
			return err
		}
		return sink.Status.ConvertFrom(ctx, source.Status)
	default:
		return apis.ConvertFromViaProxy(ctx, source, &v1beta1.Configuration{}, sink)
	}
}

// ConvertFrom helps implement apis.Convertible
func (sink *ConfigurationSpec) ConvertFrom(ctx context.Context, source v1.ConfigurationSpec) error {
	sink.Template = &RevisionTemplateSpec{}
	return sink.Template.ConvertFrom(ctx, source.Template)
}

// ConvertFrom helps implement apis.Convertible
func (sink *ConfigurationStatus) ConvertFrom(ctx context.Context, source v1.ConfigurationStatus) error {
	source.Status.ConvertTo(ctx, &sink.Status, v1.IsConfigurationCondition)

	return sink.ConfigurationStatusFields.ConvertFrom(ctx, source.ConfigurationStatusFields)
}

// ConvertFrom helps implement apis.Convertible
func (sink *ConfigurationStatusFields) ConvertFrom(ctx context.Context, source v1.ConfigurationStatusFields) error {
	sink.LatestReadyRevisionName = source.LatestReadyRevisionName
	sink.LatestCreatedRevisionName = source.LatestCreatedRevisionName
	return nil
}
