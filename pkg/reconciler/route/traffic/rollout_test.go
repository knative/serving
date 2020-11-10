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

func TestStep(t *testing.T) {
	tests := []struct {
		name            string
		prev, cur, want *Rollout
	}{{
		name: "no prev",
		cur:  &Rollout{},
		prev: nil,
		want: &Rollout{},
	}, {
		name: "prev is empty",
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
		name: "when new revision becomes 0%",
		cur: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "charlie",
				Percent:           0,
				Revisions: []RevisionRollout{{
					RevisionName: "aftermath",
					Percent:      0,
				}},
			}},
		},
		prev: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "charlie",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "your-satanic-majesties-request",
					Percent:      100,
				}},
			}},
		},
		want: &Rollout{},
	}, {
		name: "when new config is added but it's 0%",
		cur: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "charlie",
				Percent:           0,
				Revisions: []RevisionRollout{{
					RevisionName: "aftermath",
					Percent:      0,
				}},
			}, {
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
					RevisionName: "between-the-buttons",
					Percent:      100,
				}},
			}},
		},
		want: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "between-the-buttons",
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
		name: "roll with percentage change down",
		cur: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           33,
				Revisions: []RevisionRollout{{
					RevisionName: "let-it-bleed",
					Percent:      33,
				}},
			}},
		},
		prev: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           42,
				Revisions: []RevisionRollout{{
					RevisionName: "goat-head-soup",
					Percent:      11,
				}, {
					RevisionName: "aftermath",
					Percent:      31,
				}},
			}},
		},
		want: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           33,
				Revisions: []RevisionRollout{{
					RevisionName: "goat-head-soup",
					Percent:      2,
				}, {
					RevisionName: "aftermath",
					Percent:      30,
				}, {
					RevisionName: "let-it-bleed",
					Percent:      1,
				}},
			}},
		},
	}, {
		name: "roll with percentage change up",
		cur: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           75,
				Revisions: []RevisionRollout{{
					RevisionName: "let-it-bleed",
					Percent:      75,
				}},
			}},
		},
		prev: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           25,
				Revisions: []RevisionRollout{{
					RevisionName: "goat-head-soup",
					Percent:      11,
				}, {
					RevisionName: "aftermath",
					Percent:      14,
				}},
			}},
		},
		want: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           75,
				Revisions: []RevisionRollout{{
					RevisionName: "goat-head-soup",
					Percent:      11,
				}, {
					RevisionName: "aftermath",
					Percent:      63,
				}, {
					RevisionName: "let-it-bleed",
					Percent:      1,
				}},
			}},
		},
	}, {
		name: "roll, where sum < 100% (one route targets a revision, e.g.)",
		cur: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "brian",
				Percent:           70,
				Revisions: []RevisionRollout{{
					RevisionName: "let-it-bleed",
					Percent:      70,
				}},
			}},
		},
		prev: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "brian",
				Percent:           70,
				Revisions: []RevisionRollout{{
					RevisionName: "exile-on-main-st",
					Percent:      70,
				}},
			}},
		},
		want: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "brian",
				Percent:           70,
				Revisions: []RevisionRollout{{
					RevisionName: "exile-on-main-st",
					Percent:      69,
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
		name: "roll with delete (minimal config target)",
		cur: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           1,
				Revisions: []RevisionRollout{{
					RevisionName: "between-the-buttons",
					Percent:      1,
				}},
			}},
		},
		prev: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           1,
				Revisions: []RevisionRollout{{
					RevisionName: "bridges-to-babylon",
					Percent:      1,
				}},
			}},
		},
		want: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           1,
				Revisions: []RevisionRollout{{
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
		name: "new config, no roll, newer smaller",
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
	}, {
		name: "new config, no roll, newer larger",
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
				ConfigurationName: "keith",
				Percent:           100,
				Revisions: []RevisionRollout{{
					RevisionName: "black-on-blue",
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

func TestAdjustPercentage(t *testing.T) {
	tests := []struct {
		name string
		goal int
		prev *ConfigurationRollout
		want []RevisionRollout
	}{{
		name: "noop, 100%",
		goal: 100,
		prev: &ConfigurationRollout{
			Percent: 100,
			Revisions: []RevisionRollout{{
				Percent: 71,
			}, {
				Percent: 29,
			}},
		},
		want: []RevisionRollout{{
			Percent: 71,
		}, {
			Percent: 29,
		}},
	}, {
		name: "noop, 42%",
		goal: 42,
		prev: &ConfigurationRollout{
			Percent: 42,
			Revisions: []RevisionRollout{{
				Percent: 21,
			}, {
				Percent: 21,
			}},
		},
		want: []RevisionRollout{{
			Percent: 21,
		}, {
			Percent: 21,
		}},
	}, {
		name: "raise, 42% -> 75%",
		goal: 75,
		prev: &ConfigurationRollout{
			Percent: 42,
			Revisions: []RevisionRollout{{
				Percent: 21,
			}, {
				Percent: 21,
			}},
		},
		want: []RevisionRollout{{
			Percent: 21,
		}, {
			Percent: 54,
		}},
	}, {
		name: "lower, 75%->42%, lose 1",
		goal: 42,
		prev: &ConfigurationRollout{
			Percent: 75,
			Revisions: []RevisionRollout{{
				Percent: 21,
			}, {
				Percent: 54,
			}},
		},
		want: []RevisionRollout{{
			Percent: 42,
		}},
	}, {
		name: "lower, 75%->42%, lose 1, update 2, keep 3",
		goal: 42,
		prev: &ConfigurationRollout{
			Percent: 75,
			Revisions: []RevisionRollout{{
				Percent: 21,
			}, {
				Percent: 22,
			}, {
				Percent: 32,
			}},
		},
		want: []RevisionRollout{{
			Percent: 10,
		}, {
			Percent: 32,
		}},
	}, {
		name: "lower, 75%->42%, lose 2, update 3",
		goal: 42,
		prev: &ConfigurationRollout{
			Percent: 75,
			Revisions: []RevisionRollout{{
				Percent: 10,
			}, {
				Percent: 5,
			}, {
				Percent: 60,
			}},
		},
		want: []RevisionRollout{{
			Percent: 42,
		}},
	}, {
		name: "go to 0%",
		goal: 0,
		prev: &ConfigurationRollout{
			Percent: 75,
			Revisions: []RevisionRollout{{
				Percent: 10,
			}, {
				Percent: 5,
			}, {
				Percent: 60,
			}},
		},
		want: []RevisionRollout{},
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adjustPercentage(tc.goal, tc.prev)
			if got, want := tc.prev.Revisions, tc.want; !cmp.Equal(got, want) {
				t.Errorf("Rollout Mistmatch(-want,+got):\n%s", cmp.Diff(want, got))
			}
		})
	}
}
