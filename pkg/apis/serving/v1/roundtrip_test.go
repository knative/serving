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
	"testing"

	fuzz "github.com/google/gofuzz"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	pkgfuzzer "knative.dev/pkg/apis/testing/fuzzer"
	"knative.dev/pkg/apis/testing/roundtrip"
)

// fuzzerFuncs includes fuzzing funcs for knative.dev/serving v1 types
//
// For other examples see
// https://github.com/kubernetes/apimachinery/blob/master/pkg/apis/meta/fuzzer/fuzzer.go
var fuzzerFuncs = fuzzer.MergeFuzzerFuncs(
	pkgfuzzer.Funcs,
	fuzzer.MergeFuzzerFuncs(
		func(codecs serializer.CodecFactory) []interface{} {
			return []interface{}{
				func(s *ConfigurationStatus, c fuzz.Continue) {
					c.FuzzNoCustom(s) // fuzz the status object

					// Clear the random fuzzed condition
					s.Status.SetConditions(nil)

					// Fuzz the known conditions except their type value
					s.InitializeConditions()
					pkgfuzzer.FuzzConditions(&s.Status, c)
				},
				func(s *RevisionStatus, c fuzz.Continue) {
					c.FuzzNoCustom(s) // fuzz the status object

					// Clear the random fuzzed condition
					s.Status.SetConditions(nil)

					// Fuzz the known conditions except their type value
					s.InitializeConditions()
					pkgfuzzer.FuzzConditions(&s.Status, c)
				},
				func(s *RouteStatus, c fuzz.Continue) {
					c.FuzzNoCustom(s) // fuzz the status object

					// Clear the random fuzzed condition
					s.Status.SetConditions(nil)

					// Fuzz the known conditions except their type value
					s.InitializeConditions()
					pkgfuzzer.FuzzConditions(&s.Status, c)
				},
				func(s *ServiceStatus, c fuzz.Continue) {
					c.FuzzNoCustom(s) // fuzz the status object

					// Clear the random fuzzed condition
					s.Status.SetConditions(nil)

					// Fuzz the known conditions except their type value
					s.InitializeConditions()
					pkgfuzzer.FuzzConditions(&s.Status, c)
				},
				func(ps *corev1.PodSpec, c fuzz.Continue) {
					c.FuzzNoCustom(ps)

					if len(ps.Containers) == 0 {
						// There must be at least 1 container.
						ps.Containers = append(ps.Containers, corev1.Container{})
						c.Fuzz(&ps.Containers[0])
					}
				},
			}
		},
	),
)

func TestServingRoundTripTypesToJSON(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(AddToScheme(scheme))

	roundtrip.ExternalTypesViaJSON(t, scheme, fuzzerFuncs)
}
