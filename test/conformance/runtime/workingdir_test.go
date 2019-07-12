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

	"knative.dev/serving/pkg/apis/serving/v1alpha1"
	v1a1options "knative.dev/serving/pkg/testing/v1alpha1"
	"knative.dev/serving/test"
)

func withWorkingDir(wd string) v1a1options.ServiceOption {
	return func(svc *v1alpha1.Service) {
		svc.Spec.Template.Spec.Containers[0].WorkingDir = wd
	}
}

func TestWorkingDirService(t *testing.T) {
	t.Parallel()
	clients := test.Setup(t)

	const wd = "/foo/bar/baz"

	_, ri, err := fetchRuntimeInfo(t, clients, withWorkingDir(wd))
	if err != nil {
		t.Fatalf("Failed to fetch runtime info: %v", err)
	}

	if ri.Host.User.Cwd.Directory != wd {
		t.Errorf("cwd = %s, want %s, error=%s", ri.Host.User.Cwd, wd, ri.Host.User.Cwd.Error)
	}
}
