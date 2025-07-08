/*
Copyright 2025 The Knative Authors

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

package observability

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestNewFromMap(t *testing.T) {
	got, err := NewFromMap(nil)
	want := DefaultConfig()

	if err != nil {
		t.Error("unexpected error:", err)
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Error("unexpected diff (-want +got): ", diff)
	}
}

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if c == nil {
		t.Fatal("unexpected nil config")
	}

	if err := c.Validate(); err != nil {
		t.Fatal("Validate should return a valid config - got error:", err)
	}
}

func TestNewFromMapBadInput(t *testing.T) {
	cases := []struct {
		name string
		m    map[string]string
	}{{
		name: "request log template is not set",
		m: map[string]string{
			RequestLogTemplateKey: "",
			EnableRequestLogKey:   "true",
		},
	}, {
		name: "bad request template",
		m: map[string]string{
			EnableRequestLogKey:   "true",
			RequestLogTemplateKey: "{{}/* a comment */}}",
		},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewFromMap(tc.m); err == nil {
				t.Error("expected an error")
			}
		})
	}
}
