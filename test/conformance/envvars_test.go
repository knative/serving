// +build e2e

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

package conformance

import (
	"reflect"
	"testing"

	"github.com/knative/serving/test"
	corev1 "k8s.io/api/core/v1"
)

// TestShouldEnvVars verifies environment variables that are declared as "SHOULD be set" in runtime-contract
func TestShouldEnvVars(t *testing.T) {
	t.Parallel()
	clients := setup(t)
	names, ri, err := fetchRuntimeInfo(t, clients, &test.Options{})
	if err != nil {
		t.Fatal(err)
	}
	r := reflect.ValueOf(names)
	for k, v := range ShouldEnvVars {
		value, exist := ri.Host.EnvVars[k]
		if !exist {
			t.Fatalf("Runtime contract env variable %q is not set", k)
		}
		field := reflect.Indirect(r).FieldByName(v)
		if value != field.String() {
			t.Fatalf("Runtime contract env variable %q value doesn't match with expected: got %q, want %q", k, value, field.String())
		}
	}
}

// TestMustEnvVars verifies environment variables that are declared as "MUST be set" in runtime-contract
func TestMustEnvVars(t *testing.T) {
	t.Parallel()
	clients := setup(t)
	_, ri, err := fetchRuntimeInfo(t, clients, &test.Options{
		ContainerPorts: []corev1.ContainerPort{
			{ContainerPort: int32(mustEnvCustomPort)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range MustEnvVars {
		value, exist := ri.Host.EnvVars[k]
		if !exist {
			t.Fatalf("Runtime contract env variable %q is not set", k)
		}
		if v != value {
			t.Fatalf("Runtime contract env variable %q value doesn't match with expected: got %q, want %q", k, value, v)
		}
	}
}
