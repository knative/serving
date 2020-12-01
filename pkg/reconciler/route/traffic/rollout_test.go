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
	"encoding/json"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestStep(t *testing.T) {
	now := int(time.Now().UnixNano())
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
				Deadline:     2006, // <- Those should be ignored.
				LastStepTime: 2009,
				StepDuration: 2020,
				StartTime:    2004,
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
				Deadline:     1982, // <- Those should be copied.
				LastStepTime: 1984,
				StepDuration: 1988,
				StartTime:    1955,
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
				Deadline:     1982,
				LastStepTime: 1984,
				StepDuration: 1988,
				StartTime:    1955,
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
				Deadline:     1982, // <- Those should be thrown out.
				LastStepTime: 1984,
				StepDuration: 1988,
			}},
		},
		want: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "mick",
				Percent:           100,
				StartTime:         now,
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
				StartTime:         now,
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
				StartTime:         now,
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
				StartTime:         now,
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
		name: "roll with two existing revisions, no deletes",
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
				StartTime:         now - 1982, // A rollout in progress, this would be set.
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
				StartTime:         now,
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
				StartTime:         now - 1984, // A rollout in progress, this would be set.
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
				StartTime:         now,
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
		name: "a/b config, roll both",
		cur: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "keith",
				Percent:           99,
				Revisions: []RevisionRollout{{
					RevisionName: "black-on-blue",
					Percent:      99,
				}},
			}, {
				ConfigurationName: "mick",
				Percent:           1,
				Revisions: []RevisionRollout{{
					RevisionName: "it's-only-rock-n-roll",
					Percent:      1,
				}},
			}},
		},
		prev: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "keith",
				Percent:           99,
				Revisions: []RevisionRollout{{
					RevisionName: "can't-get-no-satisfaction",
					Percent:      99,
				}},
			}, {
				ConfigurationName: "mick",
				Percent:           1,
				Revisions: []RevisionRollout{{
					RevisionName: "get-off-my-cloud",
					Percent:      1,
				}},
			}},
		},
		want: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "keith",
				Percent:           99,
				StartTime:         now,
				Revisions: []RevisionRollout{{ // <-- note this one actually rolls.
					RevisionName: "can't-get-no-satisfaction",
					Percent:      98,
				}, {
					RevisionName: "black-on-blue",
					Percent:      1,
				}},
			}, {
				ConfigurationName: "mick",
				Percent:           1,
				Revisions: []RevisionRollout{{
					RevisionName: "it's-only-rock-n-roll",
					Percent:      1,
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
			got := tc.cur.Step(tc.prev, now)
			if want := tc.want; !cmp.Equal(got, want, cmpopts.EquateEmpty()) {
				t.Errorf("Wrong rolled rollout, diff(-want,+got):\n%s", cmp.Diff(want, got))
			}
			if !got.Validate() {
				t.Errorf("Step returned an invalid config:\n%#v", got)
			}
		})
	}
}

func TestObserveReady(t *testing.T) {
	const (
		now       = 200620092020 + 1982
		oldenDays = 198219841988
	)
	ro := Rollout{
		Configurations: []ConfigurationRollout{{
			ConfigurationName: "has-step",
			StepDuration:      11,
		}, {
			ConfigurationName: "no-step-no-begin",
		}, {
			ConfigurationName: "step-begin < 1s",
			StartTime:         200620092020,
		}, {
			ConfigurationName: "step-begin > 1s",
			StartTime:         oldenDays,
		}},
	}

	want := ro
	want.Configurations[2].StepDuration = 1
	want.Configurations[3].StepDuration = 3 // 2.4 rounded up.

	// This works in place.
	ro.ObserveReady(now)

	if !cmp.Equal(ro, want) {
		t.Errorf("ObserveReady generated mismatched config: diff(-want,+got):\n%s",
			cmp.Diff(want, ro))
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
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adjustPercentage(tc.goal, tc.prev)
			if got, want := tc.prev.Revisions, tc.want; !cmp.Equal(got, want, cmpopts.EquateEmpty()) {
				t.Errorf("Rollout Mistmatch(-want,+got):\n%s", cmp.Diff(want, got))
			}
		})
	}
}

func TestValidateFailures(t *testing.T) {
	tests := []struct {
		name string
		r    *Rollout
	}{{
		name: "config > 100%",
		r: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "keith",
				Percent:           101,
				Revisions: []RevisionRollout{{
					RevisionName: "black-on-blue",
					Percent:      101,
				}},
			}},
		},
	}, {
		name: "rev more than config",
		r: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "keith",
				Percent:           42,
				Revisions: []RevisionRollout{{
					RevisionName: "black-on-blue",
					Percent:      43,
				}},
			}},
		},
	}, {
		name: "revs more than config",
		r: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "keith",
				Percent:           42,
				Revisions: []RevisionRollout{{
					RevisionName: "black-on-blue",
					Percent:      41,
				}, {
					RevisionName: "smith",
					Percent:      3,
				}},
			}},
		},
	}, {
		name: "2nd config > 100%",
		r: &Rollout{
			Configurations: []ConfigurationRollout{{
				ConfigurationName: "rob",
				Percent:           10,
				Revisions: []RevisionRollout{{
					RevisionName: "roy",
					Percent:      10,
				}},
			}, {
				ConfigurationName: "keith",
				Tag:               "richards",
				Percent:           101,
				Revisions: []RevisionRollout{{
					RevisionName: "black-on-blue",
					Percent:      101,
				}},
			}},
		},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.r.Validate() {
				t.Errorf("Validate succeeded for\n%#v", tc.r)
			}
		})
	}

}

func TestConfigDone(t *testing.T) {
	r := &Rollout{
		Configurations: []ConfigurationRollout{{
			ConfigurationName: "one",
			Percent:           100,
			Revisions: []RevisionRollout{{
				RevisionName: "roy",
				Percent:      100,
			}},
		}, {
			ConfigurationName: "no",
			Percent:           0,
			Revisions:         []RevisionRollout{},
		}, {
			ConfigurationName: "many",
			Percent:           100,
			Revisions: []RevisionRollout{{
				RevisionName: "black-on-blue",
				Percent:      83,
			}, {
				RevisionName: "flowers",
				Percent:      17,
			}},
		}},
	}
	if !r.Configurations[0].Done() {
		t.Error("Single revision rollout is not `Done`")
	}
	if !r.Configurations[1].Done() {
		t.Error("Zero revisions rollout is not `Done`")
	}
	if r.Configurations[2].Done() {
		t.Error("Many revisions rollout is `Done`")
	}
}

func TestJSONRoundtrip(t *testing.T) {
	orig := &Rollout{
		Configurations: []ConfigurationRollout{{
			ConfigurationName: "one",
			Percent:           100,
			Revisions: []RevisionRollout{{
				RevisionName: "roy",
				Percent:      100,
			}},
			StartTime:    1955,
			Deadline:     2006,
			LastStepTime: 1988,
			StepDuration: 1984,
		}, {
			ConfigurationName: "no",
			Percent:           0,
			Revisions:         []RevisionRollout{},
		}, {
			ConfigurationName: "many",
			Percent:           100,
			Revisions: []RevisionRollout{{
				RevisionName: "black-on-blue",
				Percent:      83,
			}, {
				RevisionName: "flowers",
				Percent:      17,
			}},
		}},
	}

	ss, err := json.Marshal(orig)
	if err != nil {
		t.Fatal("Error serializing the rollout:", err)
	}
	deserialized := &Rollout{}
	err = json.Unmarshal(ss, deserialized)
	if err != nil {
		t.Fatal("Error deserializing proper JSON:", err)
	}
	if !cmp.Equal(deserialized, orig, cmpopts.EquateEmpty()) {
		t.Errorf("JSON roundtrip mismatch:(-want,+got)\n%s",
			cmp.Diff(orig, deserialized, cmpopts.EquateEmpty()))
	}
}
