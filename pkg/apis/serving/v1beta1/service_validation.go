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

	"github.com/knative/pkg/apis"
	"github.com/knative/serving/pkg/apis/serving"
)

// Validate makes sure that Service is properly configured.
func (s *Service) Validate(ctx context.Context) *apis.FieldError {
	// TODO(mattmoor): Add a context for passing in the parent object's name.
	return serving.ValidateObjectMetadata(s.GetObjectMeta()).ViaField("metadata").Also(
		s.Spec.Validate(withinSpec(ctx)).ViaField("spec")).Also(
		s.Status.Validate(withinStatus(ctx)).ViaField("status"))
}

// Validate implements apis.Validatable
func (ss *ServiceSpec) Validate(ctx context.Context) *apis.FieldError {
	return ss.ConfigurationSpec.Validate(ctx).Also(
		// Within the context of Service, the RouteSpec has a default
		// configurationName.
		ss.RouteSpec.Validate(withDefaultConfigurationName(ctx)))
}

// Validate implements apis.Validatable
func (ss *ServiceStatus) Validate(ctx context.Context) *apis.FieldError {
	return ss.ConfigurationStatusFields.Validate(ctx).Also(
		ss.RouteStatusFields.Validate(ctx))
}
