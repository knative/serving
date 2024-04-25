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

// All test-affecting constants should be placed in this file
// At some point it may make sense to be able to modify them
// via a configuration mechanism (see https://github.com/knative/serving/issues/6109)

package test

const (
	// ServingNamespace is the default namespace for serving e2e tests
	ServingNamespace = "serving-tests"
)
