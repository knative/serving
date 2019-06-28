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

package network

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"
)

func TestHTTPRoundTripper(t *testing.T) {
	wants := sets.NewString()
	frt := func(key string) http.RoundTripper {
		return RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
			wants.Insert(key)
			return nil, nil
		})
	}

	rt := newAutoTransport(frt("v1"), frt("v2"))

	examples := []struct {
		label      string
		protoMajor int
		want       string
	}{
		{
			label:      "use default transport for HTTP1",
			protoMajor: 1,
			want:       "v1",
		},
		{
			label:      "use h2c transport for HTTP2",
			protoMajor: 2,
			want:       "v2",
		},
		{
			label:      "use default transport for all others",
			protoMajor: 99,
			want:       "v1",
		},
	}

	for _, e := range examples {
		t.Run(e.label, func(t *testing.T) {
			wants.Delete(e.want)
			r := &http.Request{ProtoMajor: e.protoMajor}
			rt.RoundTrip(r)

			if !wants.Has(e.want) {
				t.Error("Wrong transport selected for request.")
			}
		})
	}
}

func TestDialWithBackoff(t *testing.T) {
	// Nobody's listening on a random port. Usually.
	c, err := dialWithBackOff(context.Background(), "tcp4", "127.0.0.1:41482")
	if err == nil {
		c.Close()
		t.Error("Unexpected success dialing")
	}

	// Timeout. Use special testing IP address.
	c, err = dialBackOffHelper(context.Background(), "tcp4", "198.18.0.254:8888", 2, initialTO, sleepTO)
	if err == nil {
		c.Close()
		t.Error("Unexpected success dialing")
	}
	if err != errDialTimeout {
		t.Errorf("Error = %v, want: %v", err, errDialTimeout)
	}

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer s.Close()

	c, err = dialWithBackOff(context.Background(), "tcp4", strings.TrimPrefix(s.URL, "http://"))
	if err != nil {
		t.Fatalf("dial error = %v, want nil", err)
	}
	c.Close()
}
