/*
Copyright 2020 The Knative Authors.

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

package fuzzer

import (
	"math/rand"
	"net/url"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/randfill"
)

// Funcs includes fuzzing funcs for knative.dev/serving types
//
// For other examples see
// https://github.com/kubernetes/apimachinery/blob/master/pkg/apis/meta/fuzzer/fuzzer.go
var Funcs = fuzzer.MergeFuzzerFuncs(
	func(codecs serializer.CodecFactory) []any {
		return []any{
			func(u *apis.URL, c randfill.Continue) {
				u.Scheme = randStringAtoZ(c.Rand)
				u.Host = randStringAtoZ(c.Rand)
				u.User = url.UserPassword(
					randStringAtoZ(c.Rand), // username
					randStringAtoZ(c.Rand), // password
				)
				u.RawPath = url.PathEscape(c.String(0))
				u.RawQuery = url.QueryEscape(c.String(0))
			},
		}
	},
)

// FuzzConditions fuzzes the values for the conditions. It doesn't add
// any new condition types
//
// Consumers should initialize their conditions prior to fuzzing them.
// For example:
//
//	func(s *SomeStatus, c fuzz.Continue) {
//	  c.FuzzNoCustom(s) // fuzz the status object
//
//	  // Clear the random fuzzed condition
//	  s.Status.SetConditions(nil)
//
//	  // Fuzz the known conditions except their type value
//	  s.InitializeConditions()
//	  fuzz.Conditions(&s.Status, c)
//	}
func FuzzConditions(accessor apis.ConditionsAccessor, c randfill.Continue) {
	conds := accessor.GetConditions()
	for i, cond := range conds {
		// Leave condition.Type untouched
		cond.Status = corev1.ConditionStatus(c.String(0))
		cond.Severity = apis.ConditionSeverity(c.String(0))
		cond.Message = c.String(0)
		cond.Reason = c.String(0)
		c.FillNoCustom(&cond.LastTransitionTime)
		conds[i] = cond
	}
	accessor.SetConditions(conds)
}

// taken from gofuzz internals for RandString
type charRange struct {
	first, last rune
}

func (c *charRange) choose(r *rand.Rand) rune {
	count := int64(c.last - c.first + 1)
	ch := c.first + rune(r.Int63n(count))

	return ch
}

// not fully exhaustive
func randStringAtoZ(r *rand.Rand) string {
	hostCharRange := charRange{'a', 'z'}

	n := r.Intn(20)
	runes := make([]rune, n)
	for i := range runes {
		runes[i] = hostCharRange.choose(r)
	}
	return string(runes)
}
