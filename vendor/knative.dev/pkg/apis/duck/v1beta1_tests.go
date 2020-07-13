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

package duck

import (
	"testing"

	"knative.dev/pkg/apis/duck/v1beta1"
)

// v1beta1.Conditions is an Implementable "duck type".
var _ Implementable = (*v1beta1.Conditions)(nil)

// In order for v1beta1.Conditions to be Implementable, v1beta1.KResource must be Populatable.
var _ Populatable = (*v1beta1.KResource)(nil)

// v1beta1.Source is an Implementable "duck type".
var _ Implementable = (*v1beta1.Source)(nil)

// Verify v1beta1.Source resources meet duck contracts.
var _ Populatable = (*v1beta1.Source)(nil)

// Addressable is an Implementable "duck type".
var _ Implementable = (*v1beta1.Addressable)(nil)

// Verify AddressableType resources meet duck contracts.
var _ Populatable = (*v1beta1.AddressableType)(nil)

func TestV1Beta1TypesImplements(t *testing.T) {
	testCases := []struct {
		instance interface{}
		iface    Implementable
	}{
		{instance: &v1beta1.AddressableType{}, iface: &v1beta1.Addressable{}},
		{instance: &v1beta1.KResource{}, iface: &v1beta1.Conditions{}},
	}
	for _, tc := range testCases {
		if err := VerifyType(tc.instance, tc.iface); err != nil {
			t.Error(err)
		}
	}
}
