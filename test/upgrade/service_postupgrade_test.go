// +build postupgrade

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

package upgrade

import (
	"testing"
)

func TestRunPostUpgrade(t *testing.T) {

	for name, test := range Tests {
		t.Run(name, func(t *testing.T) {
			if "dynamic" == test.clientType {
				UpdateServiceUsingDynamicClient(t, GetServiceName(test.testType))
				UpdateServiceUsingDynamicClient(t, GetScaleToZeroServiceName(test.testType))
			} else {
				UpdateService(t, GetServiceName(test.testType), test.clientType)
				UpdateService(t, GetScaleToZeroServiceName(test.testType), test.clientType)
			}
		})
	}
}
