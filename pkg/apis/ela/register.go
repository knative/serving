/*
Copyright 2018 Google LLC

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

package ela

const (
	GroupName = "elafros.dev"

	// ConfigurationLabelKey is the label key attached to a Revison indicating by
	// which Configuration it is created.
	ConfigurationLabelKey = GroupName + "/configuration"

	// ConfigurationGenerationAnnotationKey is the annotation key attached to a Revision indicating the
	// generation of the Configuration that created this revision
	ConfigurationGenerationAnnotationKey = GroupName + "/configurationGeneration"

	// RouteLabelKey is the label key attached to a Configuration indicating by
	// which Route it is configured as traffic target.
	RouteLabelKey = GroupName + "/route"

	// RevisionLabelKey is the label key attached to a Revision indicating by
	// which Revision deployment it is created.
	RevisionLabelKey = GroupName + "/revision"

	// AutoscalerLabelKey is the label key attached to a autoscaler pod indicating by
	// which Autoscaler deployment it is created.
	AutoscalerLabelKey = GroupName + "/autoscaler"
)
