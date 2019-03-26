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
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/apis"
	networking "github.com/knative/serving/pkg/apis/networking"
)

func TestServerlessServiceSpecValidation(t *testing.T) {
	tests := []struct {
		name string
		skss *ServerlessServiceSpec
		want *apis.FieldError
	}{{
		name: "valid proxy",
		skss: &ServerlessServiceSpec{
			Mode: SKSOperationModeProxy,
			Selector: map[string]string{
				"a": "b",
			},
			ProtocolType: networking.ProtocolHTTP1,
		},
		want: nil,
	}, {
		name: "valid serve",
		skss: &ServerlessServiceSpec{
			Mode: SKSOperationModeServe,
			Selector: map[string]string{
				"revision.knative.dev": "rev-1982",
			},
			ProtocolType: networking.ProtocolH2C,
		},
		want: nil,
	}, {
		name: "invalid protocol",
		skss: &ServerlessServiceSpec{
			Mode: SKSOperationModeServe,
			Selector: map[string]string{
				"revision.knative.dev": "rev-1982",
			},
			ProtocolType: networking.ProtocolType("gRPC"),
		},
		want: apis.ErrInvalidValue("gRPC", "protocolType"),
	}, {
		name: "many selectors",
		skss: &ServerlessServiceSpec{
			Mode: SKSOperationModeServe,
			Selector: map[string]string{
				"revision.knative.dev": "rev-1982",
				"activator.service":    "act-service-1988",
			},
			ProtocolType: networking.ProtocolH2C,
		},
		want: nil,
	}, {
		name: "wrong mode",
		skss: &ServerlessServiceSpec{
			Mode: "bombastic",
			Selector: map[string]string{
				"revision.knative.dev": "rev-2009",
			},
			ProtocolType: networking.ProtocolH2C,
		},
		want: apis.ErrInvalidValue("bombastic", "mode"),
	}, {
		name: "no mode",
		skss: &ServerlessServiceSpec{
			Mode: "",
			Selector: map[string]string{
				"revision.knative.dev": "rev-2009",
			},
			ProtocolType: networking.ProtocolHTTP1,
		},
		want: apis.ErrMissingField("mode"),
	}, {
		name: "no selector",
		skss: &ServerlessServiceSpec{
			Mode:         SKSOperationModeProxy,
			ProtocolType: networking.ProtocolH2C,
		},
		want: apis.ErrMissingField("selector"),
	}, {
		name: "empty selector",
		skss: &ServerlessServiceSpec{
			Mode:         SKSOperationModeProxy,
			Selector:     map[string]string{},
			ProtocolType: networking.ProtocolHTTP1,
		},
		want: apis.ErrMissingField("selector"),
	}, {
		name: "empty selector key",
		skss: &ServerlessServiceSpec{
			Mode:         SKSOperationModeProxy,
			Selector:     map[string]string{"": "n'importe quoi"},
			ProtocolType: networking.ProtocolH2C,
		},
		want: apis.ErrInvalidKeyName("", "selector", "empty key is not permitted"),
	}, {
		name: "empty selector value",
		skss: &ServerlessServiceSpec{
			Mode: SKSOperationModeProxy,
			Selector: map[string]string{
				"revision.knative.dev": "",
			},
			ProtocolType: networking.ProtocolHTTP1,
		},
		want: apis.ErrInvalidValue("", apis.CurrentField).ViaKey("revision.knative.dev").ViaField("selector"),
	}, {
		name: "multiple errors",
		skss: &ServerlessServiceSpec{
			Selector: map[string]string{
				"revision.knative.dev": "",
			},
			ProtocolType: networking.ProtocolH2C,
		},
		want: apis.ErrMissingField("mode").Also(apis.ErrInvalidValue("", apis.CurrentField).ViaKey("revision.knative.dev").ViaField("selector")),
	}, {
		name: "empty spec",
		skss: &ServerlessServiceSpec{},
		want: apis.ErrMissingField(apis.CurrentField),
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.skss.Validate(context.Background())
			if diff := cmp.Diff(test.want.Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
			// Now validate via parent object
			got = (&ServerlessService{
				Spec: *test.skss,
			}).Validate(context.Background())
			if diff := cmp.Diff(test.want.ViaField("spec").Error(), got.Error()); diff != "" {
				t.Errorf("Validate (-want, +got) = %v", diff)
			}
		})
	}
}
