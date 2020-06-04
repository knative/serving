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

package v1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	"knative.dev/pkg/apis"
)

var configCondSet = apis.NewLivingConditionSet()

// GetConditionSet retrieves the condition set for this resource. Implements the KRShaped interface.
func (*Configuration) GetConditionSet() apis.ConditionSet {
	return configCondSet
}

// GetGroupVersionKind returns the GroupVersionKind.
func (*Configuration) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("Configuration")
}

// IsReady returns true if the Status condition ConfigurationConditionReady
// is true and the latest spec has been observed.
func (c *Configuration) IsReady() bool {
	cs := c.Status
	return cs.ObservedGeneration == c.Generation &&
		cs.GetCondition(ConfigurationConditionReady).IsTrue()
}

// IsFailed returns true if the resource has observed
// the latest generation and ready is false.
func (c *Configuration) IsFailed() bool {
	cs := c.Status
	return cs.ObservedGeneration == c.Generation &&
		cs.GetCondition(ConfigurationConditionReady).IsFalse()
}

// IsLatestReadyRevisionNameUpToDate returns true if the Configuration is ready
// and LatestCreateRevisionName is equal to LatestReadyRevisionName. Otherwise
// it returns false.
func (c *Configuration) IsLatestReadyRevisionNameUpToDate() bool {
	return c.IsReady() &&
		c.Status.LatestCreatedRevisionName == c.Status.LatestReadyRevisionName
}

// InitializeConditions sets the initial values to the conditions.
func (cs *ConfigurationStatus) InitializeConditions() {
	configCondSet.Manage(cs).InitializeConditions()
}

// GetTemplate returns a pointer to the relevant RevisionTemplateSpec field.
// It is never nil and should be exactly the specified template as guaranteed
// by validation.
func (cs *ConfigurationSpec) GetTemplate() *RevisionTemplateSpec {
	return &cs.Template
}

func (cs *ConfigurationStatus) SetLatestCreatedRevisionName(name string) {
	cs.LatestCreatedRevisionName = name
	if cs.LatestReadyRevisionName != name {
		configCondSet.Manage(cs).
			MarkUnknown(ConfigurationConditionReady, "", "")
	}
}

func (cs *ConfigurationStatus) SetLatestReadyRevisionName(name string) {
	cs.LatestReadyRevisionName = name
	if cs.LatestReadyRevisionName == cs.LatestCreatedRevisionName {
		configCondSet.Manage(cs).MarkTrue(ConfigurationConditionReady)
	}
}

func (cs *ConfigurationStatus) MarkLatestCreatedFailed(name, message string) {
	configCondSet.Manage(cs).MarkFalse(
		ConfigurationConditionReady,
		"RevisionFailed",
		"Revision %q failed with message: %s.", name, message)
}

func (cs *ConfigurationStatus) MarkRevisionCreationFailed(message string) {
	configCondSet.Manage(cs).MarkFalse(
		ConfigurationConditionReady,
		"RevisionFailed",
		"Revision creation failed with message: %s.", message)
}

func (cs *ConfigurationStatus) MarkLatestReadyDeleted() {
	configCondSet.Manage(cs).MarkFalse(
		ConfigurationConditionReady,
		"RevisionDeleted",
		"Revision %q was deleted.", cs.LatestReadyRevisionName)
}
