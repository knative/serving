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
package v1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis"
)

func TestIsConfigurationCondition(t *testing.T) {
	cType := apis.ConditionType("DefinitelyNotConfigurationType")

	if IsConfigurationCondition(cType) {
		t.Error("Not expected to be a configuration type")
	}

	if !IsConfigurationCondition(ConfigurationConditionReady) {
		t.Error("Expected to be a configuration type")
	}
}

func TestConfigurationGetStatus(t *testing.T) {
	r := &Configuration{
		Status: ConfigurationStatus{},
	}
	want := &r.Status.Status
	if got := r.GetStatus(); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}

func TestConfigurationGetObjectMeta(t *testing.T) {
	r := &Configuration{
		TypeMeta: metav1.TypeMeta{},
	}
	want := &r.TypeMeta
	if got := r.GetTypeMeta(); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}
