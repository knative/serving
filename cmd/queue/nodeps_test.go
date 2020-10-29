/*
Copyright 2020 The Knative Authors

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

package main_test

import (
	"testing"

	"knative.dev/pkg/depcheck"
)

func TestNoDeps(t *testing.T) {
	depcheck.AssertNoDependency(t, map[string][]string{
		"knative.dev/serving/cmd/queue": {
			"k8s.io/apimachinery/pkg/api/apitesting/fuzzer",
			// TODO(https://github.com/knative/serving/issues/9957): Remove these dependencies.
			// "k8s.io/client-go/informers",
			// "k8s.io/client-go/kubernetes",
		},
	})
}
