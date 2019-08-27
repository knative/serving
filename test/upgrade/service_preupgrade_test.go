// +build preupgrade

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

func TestRunPreUpgrade(t *testing.T) {

	for name, test := range Tests {
		switch test.clientType {
		case "v1alpha1":
			t.Run(name, func(t *testing.T) {
				testType := test.testType
				CreateServiceUsingV1Alpha1Client(t, GetServiceName(testType))
				CreateServiceAndScaleToZeroUsingV1Alpha1Client(t, GetScaleToZeroServiceName(testType))
			})
		case "v1beta1":
			t.Run(name, func(t *testing.T) {
				testType := test.testType
				CreateServiceUsingV1Beta1Client(t, GetServiceName(testType))
				CreateServiceAndScaleToZeroUsingV1Beta1Client(t, GetScaleToZeroServiceName(testType))
			})
		case "dynamic":
			t.Run(name, func(t *testing.T) {
				testType := test.testType
				CreateServiceUsingDynamicClient(t, GetServiceName(testType))
				CreateServiceAndScaleToZeroUsingDynamicClient(t, GetScaleToZeroServiceName(testType))
			})
		}

	}
}
