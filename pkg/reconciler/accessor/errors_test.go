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
		err: Error{
			err: fmt.Errorf("test error"),
		},
		want: false,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsNotOwned(tc.err)
			if tc.want != got {
				t.Errorf("IsNotOwned function fails. want: %t, got: %t", tc.want, got)
			}
		})
	}
}

func TestError(t *testing.T) {
	err := Error{
		err:         fmt.Errorf("test error"),
		errorReason: NotOwnResource,
	}
	got := err.Error()
	want := "notowned: test error"
	if got != want {
		t.Errorf("Error function fails. want: %q, got: %q", want, got)
	}

}
