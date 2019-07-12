// +build e2e

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

package runtime

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	v1a1options "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
)

func withPort(name string) v1a1options.ServiceOption {
	return func(s *v1alpha1.Service) {
		if name != "" {
			s.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{{Name: name}}
		}
	}
}

func TestProtocols(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		portName  string
		wantMajor int
		wantMinor int
	}{{
		name:      "h2c",
		portName:  "h2c",
		wantMajor: 2,
		wantMinor: 0,
	}, {
		name:      "http1",
		portName:  "http1",
		wantMajor: 1,
		wantMinor: 1,
	}, {
		name:      "default",
		portName:  "",
		wantMajor: 1,
		wantMinor: 1,
	}}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			clients := test.Setup(t)
			_, ri, err := fetchRuntimeInfo(t, clients, withPort(tt.portName))
			if err != nil {
				t.Fatalf("Failed to fetch runtime info: %v", err)
			}

			if tt.wantMajor != ri.Request.ProtoMajor || tt.wantMinor != ri.Request.ProtoMinor {
				t.Errorf("Want HTTP/%d.%d, got HTTP/%d.%d", tt.wantMajor, tt.wantMinor, ri.Request.ProtoMajor, ri.Request.ProtoMinor)
			}
		})
	}
}
