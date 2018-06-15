/*
Copyright 2018 Google LLC. All rights reserved.
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
package v1alpha1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/runtime"
)

var testCases = []struct {
	name   string
	before runtime.Object
	after  runtime.Object
}{

	{
		name:   "Revision",
		before: &Revision{},
		after: &Revision{
			Spec: RevisionSpec{
				ServingState:     RevisionServingStateActive,
				ConcurrencyModel: RevisionRequestConcurrencyModelMulti,
			},
		},
	},

	{
		name:   "Configuration",
		before: &Configuration{},
		after: &Configuration{
			Spec: ConfigurationSpec{
				RevisionTemplate: RevisionTemplateSpec{
					Spec: RevisionSpec{
						ConcurrencyModel: RevisionRequestConcurrencyModelMulti,
					},
				},
			},
		},
	},

	{
		name: "Service RunLatest",
		before: &Service{
			Spec: ServiceSpec{
				RunLatest: &RunLatestType{},
			},
		},
		after: &Service{
			Spec: ServiceSpec{
				RunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{
								ConcurrencyModel: RevisionRequestConcurrencyModelMulti,
							},
						},
					},
				},
			},
		},
	},

	{
		name: "Service Pinned",
		before: &Service{
			Spec: ServiceSpec{
				Pinned: &PinnedType{},
			},
		},
		after: &Service{
			Spec: ServiceSpec{
				Pinned: &PinnedType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{
								ConcurrencyModel: RevisionRequestConcurrencyModelMulti,
							},
						},
					},
				},
			},
		},
	},
}

func TestDefaults(t *testing.T) {
	var testScheme = getTestScheme()

	for _, tc := range testCases {
		testScheme.Default(tc.before)
		if diff := cmp.Diff(tc.after, tc.before); diff != "" {
			t.Errorf("%s: Unexpected default (-want +got): %v", tc.name, diff)
		}
	}
}

// We can't import the generated scheme because it depends on this package,
// creating an import cycle.
func getTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		panic(err)
	}
	return scheme
}
