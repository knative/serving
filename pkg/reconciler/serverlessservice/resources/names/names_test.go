/*
Copyright 2021 The Knative Authors

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

package names

import (
	"strings"
	"testing"
)

func TestNamer(t *testing.T) {
	tests := []struct {
		name string
		in   string
		f    func(string) string
		want string
	}{{
		name: "Private Service too long",
		in:   strings.Repeat("f", 63),
		f:    PrivateService,
		want: "fffffffffffffffffffffff105d7597f637e83cc711605ac3ea4957-private",
	}, {
		name: "Private Service long enough",
		in:   strings.Repeat("f", 55),
		f:    PrivateService,
		want: strings.Repeat("f", 55) + "-private",
	}, {
		name: "Private Service",
		in:   "foo",
		f:    PrivateService,
		want: "foo-private",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.f(test.in)
			if got != test.want {
				t.Errorf("%s() = %v, wanted %v", test.name, got, test.want)
			}
		})
	}
}
