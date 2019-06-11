/*
Copyright 2018 The Knative Authors

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

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/apis/duck"
	duckv1beta1 "github.com/knative/pkg/apis/duck/v1alpha1"
)

func TestClusterIngressDuckTypes(t *testing.T) {
	tests := []struct {
		name string
		t    duck.Implementable
	}{{
		name: "conditions",
		t:    &duckv1beta1.Conditions{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := duck.VerifyType(&ClusterIngress{}, test.t)
			if err != nil {
				t.Errorf("VerifyType(ClusterIngress, %T) = %v", test.t, err)
			}
		})
	}
}

func TestCIGetGroupVersionKind(t *testing.T) {
	ci := ClusterIngress{}
	expected := SchemeGroupVersion.WithKind("ClusterIngress")
	if diff := cmp.Diff(expected, ci.GetGroupVersionKind()); diff != "" {
		t.Errorf("Unexpected diff (-want, +got) = %v", diff)
	}
}

func TestIsPublic(t *testing.T) {
	ci := ClusterIngress{}
	if !ci.IsPublic() {
		t.Error("Expected default ClusterIngress to be public, for backward compatibility")
	}
	if !ci.IsPublic() {
		t.Errorf("Expected IsPublic()==true, saw %v", ci.IsPublic())
	}
	ci.Spec.Visibility = IngressVisibilityExternalIP
	if !ci.IsPublic() {
		t.Errorf("Expected IsPublic()==true, saw %v", ci.IsPublic())
	}
	ci.Spec.Visibility = IngressVisibilityClusterLocal
	if ci.IsPublic() {
		t.Errorf("Expected IsPublic()==false, saw %v", ci.IsPublic())
	}

}
