/*
Copyright 2017 The Knative Authors
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
)

func TestFieldError(t *testing.T) {
	tests := []struct {
		name     string
		err      *FieldError
		prefixes [][]string
		want     string
	}{{
		name: "simple single no propagation",
		err: &FieldError{
			Message: "hear me roar",
			Paths:   []string{"foo.bar"},
		},
		want: "hear me roar: foo.bar",
	}, {
		name: "simple single propagation",
		err: &FieldError{
			Message: `invalid value "blah"`,
			Paths:   []string{"foo"},
		},
		prefixes: [][]string{{"bar"}, {"baz", "ugh"}, {"hoola"}},
		want:     `invalid value "blah": hoola.baz.ugh.bar.foo`,
	}, {
		name: "simple multiple propagation",
		err: &FieldError{
			Message: "invalid field(s)",
			Paths:   []string{"foo", "bar"},
		},
		prefixes: [][]string{{"baz", "ugh"}},
		want:     "invalid field(s): baz.ugh.foo, baz.ugh.bar",
	}, {
		name: "multiple propagation with details",
		err: &FieldError{
			Message: "invalid field(s)",
			Paths:   []string{"foo", "bar"},
			Details: `I am a long
long
loooong
Body.`,
		},
		prefixes: [][]string{{"baz", "ugh"}},
		want: `invalid field(s): baz.ugh.foo, baz.ugh.bar
I am a long
long
loooong
Body.`,
	}, {
		name: "single propagation, empty start",
		err: &FieldError{
			Message: "invalid field(s)",
			// We might see this validating a scalar leaf.
			Paths: []string{currentField},
		},
		prefixes: [][]string{{"baz", "ugh"}},
		want:     "invalid field(s): baz.ugh",
	}, {
		name: "single propagation, no paths",
		err: &FieldError{
			Message: "invalid field(s)",
			Paths:   nil,
		},
		prefixes: [][]string{{"baz", "ugh"}},
		want:     "invalid field(s): ",
	}, {
		name:     "nil propagation",
		err:      nil,
		prefixes: [][]string{{"baz", "ugh"}},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fe := test.err
			// Simulate propagation up a call stack.
			for _, prefix := range test.prefixes {
				fe = fe.ViaField(prefix...)
			}
			if test.want != "" {
				got := fe.Error()
				if got != test.want {
					t.Errorf("Error() = %v, wanted %v", got, test.want)
				}
			} else if fe != nil {
				t.Errorf("ViaField() = %v, wanted nil", fe)
			}
		})
	}
}
