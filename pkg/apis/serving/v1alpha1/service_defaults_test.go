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

package v1alpha1

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestServiceDefaulting(t *testing.T) {
	tests := []struct {
		name string
		in   *Service
		want *Service
	}{{
		name: "empty",
		in:   &Service{},
		// Do nothing when no type is provided
		want: &Service{},
	}, {
		name: "manual",
		in: &Service{
			Spec: ServiceSpec{
				Manual: &ManualType{},
			},
		},
		// Manual does not take a configuration so do nothing
		want: &Service{
			Spec: ServiceSpec{
				Manual: &ManualType{},
			},
		},
	}, {
		name: "run latest",
		in: &Service{
			Spec: ServiceSpec{
				RunLatest: &RunLatestType{},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				RunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{},
						},
					},
				},
			},
		},
	}, {
		name: "run latest - no overwrite",
		in: &Service{
			Spec: ServiceSpec{
				RunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{
								ContainerConcurrency: 1,
							},
						},
					},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				RunLatest: &RunLatestType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{
								ContainerConcurrency: 1,
							},
						},
					},
				},
			},
		},
	}, {
		name: "pinned",
		in: &Service{
			Spec: ServiceSpec{
				Pinned: &PinnedType{},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				Pinned: &PinnedType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{},
						},
					},
				},
			},
		},
	}, {
		name: "pinned - no overwrite",
		in: &Service{
			Spec: ServiceSpec{
				Pinned: &PinnedType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{
								ContainerConcurrency: 1,
							},
						},
					},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				Pinned: &PinnedType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{
								ContainerConcurrency: 1,
							},
						},
					},
				},
			},
		},
	}, {
		name: "release",
		in: &Service{
			Spec: ServiceSpec{
				Release: &ReleaseType{},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				Release: &ReleaseType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{},
						},
					},
				},
			},
		},
	}, {
		name: "release - no overwrite",
		in: &Service{
			Spec: ServiceSpec{
				Release: &ReleaseType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{
								ContainerConcurrency: 1,
							},
						},
					},
				},
			},
		},
		want: &Service{
			Spec: ServiceSpec{
				Release: &ReleaseType{
					Configuration: ConfigurationSpec{
						RevisionTemplate: RevisionTemplateSpec{
							Spec: RevisionSpec{
								ContainerConcurrency: 1,
							},
						},
					},
				},
			},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.in
			got.SetDefaults()
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("SetDefaults (-want, +got) = %v", diff)
			}
		})
	}
}
