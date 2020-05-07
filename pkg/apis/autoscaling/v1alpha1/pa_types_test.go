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
package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPodAutoscalerGetStatus(t *testing.T) {
	r := &PodAutoscaler{
		Status: PodAutoscalerStatus{},
	}
	want := &r.Status.Status
	if got := r.GetStatus(); got != want {
		t.Errorf("GotStatus=%v, want=%v", got, want)
	}
}

func TestPodAutoscalerGetObjectMeta(t *testing.T) {
	r := &PodAutoscaler{
		TypeMeta: metav1.TypeMeta{},
	}
	want := &r.TypeMeta
	if got := r.GetTypeMeta(); got != want {
		t.Errorf("GetTypeMeta=%v, want=%v", got, want)
	}
}
