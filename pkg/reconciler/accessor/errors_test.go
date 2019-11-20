/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package accessor

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsNotOwned(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{{
		name: "IsNotOwned error",
		err: Error{
			err:         fmt.Errorf("test error"),
			errorReason: NotOwnResource,
		},
		want: true,
	}, {
		name: "other error",
		err:  errors.New("test error"),
		want: false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNotOwned(tc.err); tc.want != got {
				t.Errorf("IsNotOwned(%v) = %v, want = %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestError(t *testing.T) {
	err := Error{
		err:         fmt.Errorf("test error"),
		errorReason: NotOwnResource,
	}
	if got, want := err.Error(), "notowned: test error"; got != want {
		t.Errorf("Error() = %q, want = %q", got, want)
	}
}

func TestNewAccessorError(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		reason string
		want   string
	}{{
		name:   "error with reason",
		err:    errors.New("test error"),
		reason: NotOwnResource,
		want:   "notowned: test error",
	}, {
		name:   "error with no reason",
		err:    errors.New("test error"),
		reason: "",
		want:   ": test error",
	}, {
		name:   "error with no message and with reason",
		err:    errors.New(""),
		reason: NotOwnResource,
		want:   "notowned: ",
	}, {
		name:   "error with no message and reason",
		err:    errors.New(""),
		reason: "",
		want:   ": ",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NewAccessorError(tc.err, tc.reason); got.Error() != tc.want {
				t.Errorf("NewAccessorError() = %q, want = %q", got.Error(), tc.want)
			}
		})
	}
}
