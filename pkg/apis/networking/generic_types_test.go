/*
Copyright 2019 The Knative Authors

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

package networking

import (
	"context"
	"reflect"
	"testing"

	"knative.dev/pkg/apis"
)

func TestProtocolTypeValidate(t *testing.T) {
	cases := []struct {
		name   string
		proto  ProtocolType
		expect *apis.FieldError
	}{{
		name:   "no protocol",
		proto:  "",
		expect: apis.ErrMissingField(apis.CurrentField),
	}, {
		name:   "invalid protocol",
		proto:  "invalidProtocol",
		expect: apis.ErrInvalidValue("invalidProtocol", apis.CurrentField),
	}, {
		name:   "valid h2c protocol",
		proto:  ProtocolH2C,
		expect: nil,
	}, {
		name:   "valid http1 protocol",
		proto:  ProtocolHTTP1,
		expect: nil,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, want := c.proto.Validate(context.Background()), c.expect; !reflect.DeepEqual(got, want) {
				t.Errorf("got = %v, want: %v", got, want)
			}
		})
	}
}
