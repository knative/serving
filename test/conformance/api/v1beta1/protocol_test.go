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

package v1beta1

import (
	"encoding/json"
	"testing"

	"github.com/knative/serving/pkg/apis/serving/v1beta1"
	v1b1options "github.com/knative/serving/pkg/testing/v1beta1"
	"github.com/knative/serving/test"
	"github.com/knative/serving/test/types"
	v1b1test "github.com/knative/serving/test/v1beta1"
	corev1 "k8s.io/api/core/v1"
	pkgTest "knative.dev/pkg/test"
)

func withPort(name string) v1b1options.ServiceOption {
	return func(s *v1beta1.Service) {
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
			names := &test.ResourceNames{
				Service: test.ObjectNameForTest(t),
				Image:   test.Runtime,
			}

			objects, err := v1b1test.CreateServiceReady(t, clients, names, withPort(tt.portName))
			if err != nil {
				t.Fatalf("Failed to create service: %v", err)
			}

			resp, err := pkgTest.WaitForEndpointState(
				clients.KubeClient,
				t.Logf,
				objects.Service.Status.URL.Host,
				v1b1test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
				"Protocols",
				test.ServingFlags.ResolvableDomain)
			if err != nil {
				t.Fatalf("Error probing domain %s: %v", objects.Service.Status.URL.Host, err)
			}

			var ri types.RuntimeInfo
			err = json.Unmarshal(resp.Body, &ri)

			if tt.wantMajor != ri.Request.ProtoMajor || tt.wantMinor != ri.Request.ProtoMinor {
				t.Errorf("Want HTTP/%d.%d, got HTTP/%d.%d", tt.wantMajor, tt.wantMinor, ri.Request.ProtoMajor, ri.Request.ProtoMinor)
			}
		})
	}
}
