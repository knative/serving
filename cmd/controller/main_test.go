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

package main

import (
	"testing"

	"knative.dev/serving/test/ha"
)

func TestNumController(t *testing.T) {
	if got, want := len(ctors), ha.NumControllerReconcilers; got != want {
		t.Errorf("Unexpected number of controller = %d, wanted %d.  This likely means the constant should be updated.", got, want)
	}
}
