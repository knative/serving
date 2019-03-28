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

package utils

import (
	"os"
	"testing"
)

func TestParseEnvStrs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want map[string]string
	}{{
		name: "empty input",
		in:   []string{},
		want: map[string]string{},
	}, {
		name: "happy path single item",
		in:   []string{"test1=t"},
		want: map[string]string{
			"test1": "t",
		},
	}, {
		name: "single item with empty value",
		in:   []string{"test1="},
		want: map[string]string{
			"test1": "",
		},
	}, {
		name: "single item with no equals",
		in:   []string{"test1"},
		want: map[string]string{
			"test1": "",
		},
	}, {
		name: "single item with no name",
		in:   []string{"=t"},
		want: map[string]string{},
	}, {
		name: "single item with multiple equals",
		in:   []string{"test1=t=y"},
		want: map[string]string{
			"test1": "t=y",
		},
	}, {
		name: "single item with consecutive equals",
		in:   []string{"test1==t="},
		want: map[string]string{
			"test1": "=t=",
		},
	}, {
		name: "only one equals",
		in:   []string{"="},
		want: map[string]string{},
	}, {
		name: "multiple equals",
		in:   []string{"==="},
		want: map[string]string{},
	}, {
		name: "multiple items",
		in: []string{
			"test1=t=y",
			"test2=2",
			"=",
			"test3=",
			"test4 = ",
			"=test5",
		},
		want: map[string]string{
			"test1":  "t=y",
			"test2":  "2",
			"test3":  "",
			"test4 ": " ",
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out := parseEnvStrs(test.in)
			if got, want := len(out), len(test.want); got != want {
				t.Errorf("Expected a map with %v items, got a map with %v items.", want, got)
			}
			for k, got := range out {
				if want := test.want[k]; got != want {
					t.Errorf("Expected value of key %v to be %v but got %v.", k, want, got)
				}
			}
		})
	}
}

func TestGetCachedEnv(t *testing.T) {
	m := parseEnvStrs(os.Environ())
	if got, want := len(m), len(os.Environ()); want != got {
		t.Errorf("Expected a map with %v items, got a map with %v items.", want, got)
	}
	for k, got := range m {
		if want := os.Getenv(k); want != got {
			t.Errorf("Expected value of key %v to be %v but got %v.", k, want, got)
		}
		if want := GetCachedEnv(k); want != got {
			t.Errorf("Expected value of cached key %v to be %v but got %v.", k, want, got)
		}
	}
}
