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

func TestServerlessServiceGetStatus(t *testing.T) {
	r := &ServerlessService{
		Status: ServerlessServiceStatus{},
	}

	if got, want := r.GetStatus(), &r.Status.Status; got != want {
		t.Errorf("GotStatus=%v, want=%v", got, want)
	}
}

func TestServerlessServiceGetObjectMeta(t *testing.T) {
	r := &ServerlessService{
		TypeMeta: metav1.TypeMeta{},
	}

	if got, want := r.GetTypeMeta(), &r.TypeMeta; got != want {
		t.Errorf("GotTypeMeta=%v, want=%v", got, want)
	}
}
