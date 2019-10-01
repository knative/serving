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
	"testing"
)

func TestServicePortName(t *testing.T) {
	cases := []struct {
		name   string
		proto  ProtocolType
		expect string
	}{{
		name:   "pass h2c get http2 protocol",
		proto:  ProtocolH2C,
		expect: ServicePortNameH2C,
	}, {
		name:   "pass any get http protocol",
		proto:  ProtocolHTTP1,
		expect: ServicePortNameHTTP1,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, want := ServicePortName(c.proto), c.expect; !(got == want) {
				t.Errorf("got = %s, want: %s", got, want)
			}
		})
	}
}

func TestServicePort(t *testing.T) {
	cases := []struct {
		name   string
		proto  ProtocolType
		expect int
	}{{
		name:   "pass h2c protocol to get Serving and Activator K8s services for HTTP/2 endpoints",
		proto:  ProtocolH2C,
		expect: ServiceHTTP2Port,
	}, {
		name:   "pass any protocol to get Serving and Activator K8s services for HTTP/1 endpoints",
		proto:  ProtocolHTTP1,
		expect: ServiceHTTPPort,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, want := ServicePort(c.proto), c.expect; !(got == want) {
				t.Errorf("got = %d, want: %d", got, want)
			}
		})
	}
}
