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

package v1

import (
	fuzz "github.com/google/gofuzz"
	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"knative.dev/pkg/apis"
	pkgfuzzer "knative.dev/pkg/apis/testing/fuzzer"
)

var testConditions = apis.Conditions{{Type: apis.ConditionReady}, {Type: apis.ConditionSucceeded}}

// FuzzerFuncs includes fuzzing funcs for knative.dev/duck v1 types
// In particular it makes sure that Delivery has only valid BackoffPolicyType in it.
//
// For other examples see
// https://github.com/kubernetes/apimachinery/blob/master/pkg/apis/meta/fuzzer/fuzzer.go
var FuzzerFuncs = fuzzer.MergeFuzzerFuncs(
	func(codecs serializer.CodecFactory) []interface{} {
		return []interface{}{
			func(status *Status, c fuzz.Continue) {
				c.FuzzNoCustom(status) // fuzz the Status
				status.SetConditions(testConditions)
				pkgfuzzer.FuzzConditions(status, c)
			},
		}
	},
)
