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
package activator

import (
	"testing"

	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
)

func TestServicePort(t *testing.T) {
	tests := []struct {
		name     string
		protocol v1alpha1.RevisionProtocolType
		port     int32
	}{{
		name:     "h2c",
		protocol: v1alpha1.RevisionProtocolH2C,
		port:     ServicePortH2C,
	}, {
		name:     "http1",
		protocol: v1alpha1.RevisionProtocolHTTP1,
		port:     ServicePortHTTP1,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := tt.port
			got := ServicePort(tt.protocol)

			if want != got {
				t.Errorf("unexpected port. want %d, got %d", want, got)
			}
		})
	}
}
