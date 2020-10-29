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

// rollout.go contains the types and functions to deal with
// gradual rollout of the new revision for a configuration target.
// The types in this file are expected to be serialized as strings
// and used as annotations for progressive rollout logic.

package traffic

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRoll(t *testing.T) {
	tests := []struct {
		name            string
		prev, cur, want *Rollout
	}{{
		name: "no prev",
		cur:  &Rollout{},
		prev: nil,
		want: &Rollout{},
	}, {
		name: "simplest, same",
		cur: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "let-it-bleed",
					Percent:      100,
				}},
			}},
		},
		prev: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "let-it-bleed",
					Percent:      100,
				}},
			}},
		},
		want: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "let-it-bleed",
					Percent:      100,
				}},
			}},
		},
	}, {
		name: "simplest, roll",
		cur: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "let-it-bleed",
					Percent:      100,
				}},
			}},
		},
		prev: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "goat-head-soup",
					Percent:      100,
				}},
			}},
		},
		want: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "goat-head-soup",
					Percent:      99,
				}, {
					RevisionName: "let-it-bleed",
					Percent:      1,
				}},
			}},
		},
	}, {
		name: "roll with two existing, no deletes",
		cur: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "sticky-fingers",
					Percent:      100,
				}},
			}},
		},
		prev: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "goat-head-soup",
					Percent:      95,
				}, {
					RevisionName: "beggars-banquet",
					Percent:      5, // 5 should become 4.
				}},
			}},
		},
		want: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "goat-head-soup",
					Percent:      95,
				}, {
					RevisionName: "beggars-banquet",
					Percent:      4,
				}, {
					RevisionName: "sticky-fingers",
					Percent:      1,
				}},
			}},
		},
	}, {
		name: "roll with delete (two fast successive rolls)",
		cur: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "between-the-buttons",
					Percent:      100,
				}},
			}},
		},
		prev: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "goat-head-soup",
					Percent:      99,
				}, {
					RevisionName: "bridges-to-babylon",
					Percent:      1,
				}},
			}},
		},
		want: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "goat-head-soup",
					Percent:      99,
				}, {
					RevisionName: "between-the-buttons",
					Percent:      1,
				}},
			}},
		},
	}, {
		name: "new tag, no roll", // just attached a tag to an existing route.
		cur: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "black-on-blue",
					Percent:      100,
				}},
			}, {
				ConfigurationName: "mick",
				Tag:               "jagger",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "black-on-blue",
					Percent:      100,
				}},
			}},
		},
		prev: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "black-on-blue",
					Percent:      100,
				}},
			}},
		},
		want: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "black-on-blue",
					Percent:      100,
				}},
			}, {
				ConfigurationName: "mick",
				Tag:               "jagger",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "black-on-blue",
					Percent:      100,
				}},
			}},
		},
	}, {
		name: "deleted config, no roll",
		cur: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "it's-only-rock-n-roll",
					Percent:      100,
				}},
			}},
		},
		prev: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "keith",
				Percent:           50,
				Revisions: []RevisionRollout{{
					RevisionName: "black-on-blue",
					Percent:      50,
				}},
			}, {
				ConfigurationName: "mick",
				Percent:           50,
				Revisions: []RevisionRollout{{
					RevisionName: "it's-only-rock-n-roll",
					Percent:      50,
				}},
			}},
		},
		want: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "it's-only-rock-n-roll",
					Percent:      100,
				}},
			}},
		},
	}, {
		name: "new config, no roll",
		cur: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "keith",
				Percent:           50,
				Revisions: []RevisionRollout{{
					RevisionName: "black-on-blue",
					Percent:      50,
				}},
			}, {
				ConfigurationName: "mick",
				Percent:           50,
				Revisions: []RevisionRollout{{
					RevisionName: "it's-only-rock-n-roll",
					Percent:      50,
				}},
			}},
		},
		prev: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "it's-only-rock-n-roll",
					Percent:      100,
				}},
			}},
		},
		want: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "keith",
				Percent:           50,
				Revisions: []RevisionRollout{{
					RevisionName: "black-on-blue",
					Percent:      50,
				}},
			}, {
				ConfigurationName: "mick",
				Percent:           50,
				Revisions: []RevisionRollout{{
					RevisionName: "it's-only-rock-n-roll",
					Percent:      50,
				}},
			}},
		},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got, want := tc.cur.Step(tc.prev), tc.want; !cmp.Equal(got, want) {
				t.Errorf("Wrong rolled rollout, diff(-want,+got):\n%s", cmp.Diff(want, got))
			}
		})
	}
}
