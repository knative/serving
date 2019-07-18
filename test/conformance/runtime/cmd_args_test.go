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

	"github.com/google/go-cmp/cmp"
	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	v1a1options "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
)

func withCmdArgs(cmds []string, args []string) v1a1options.ServiceOption {
	return func(s *v1alpha1.Service) {
		c := &s.Spec.Template.Spec.Containers[0]
		c.Command = cmds
		c.Args = args
	}
}

func TestCmdArgs(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	cmds := []string{"/ko-app/runtime", "abra"}
	args := []string{"cadabra", "do"}

	_, ri, err := fetchRuntimeInfo(t, clients, withCmdArgs(cmds, args))
	if err != nil {
		t.Fatalf("Failed to fetch runtime info: %v", err)
	}

	want := append(cmds, args...)
	if !cmp.Equal(ri.Host.Args, want) {
		t.Errorf("args = %v, want: %v", ri.Host.Args, want)
	}
}
