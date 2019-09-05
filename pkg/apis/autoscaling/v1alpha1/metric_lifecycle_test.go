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
package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"knative.dev/serving/pkg/apis/autoscaling"
)

func TestMetricGetGroupVersionKind(t *testing.T) {
	m := &Metric{}
	want := schema.GroupVersionKind{
		Group:   autoscaling.InternalGroupName,
		Version: "v1alpha1",
		Kind:    "Metric",
	}
	if got := m.GetGroupVersionKind(); got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}
