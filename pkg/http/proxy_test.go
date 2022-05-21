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

package http

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"

	netheader "knative.dev/networking/pkg/http/header"
)

func TestNewHeaderPruningProxy(t *testing.T) {
	var handler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		r.Header.Add("Host", r.Host) // Explicitly add the host header so we can assert.
		if err := json.NewEncoder(w).Encode(r.Header); err != nil {
			panic(err)
		}
	}

	server := httptest.NewServer(handler)
	serverURL, _ := url.Parse(server.URL)

	defer server.Close()

	tests := []struct {
		name          string
		url           string
		host          string
		header        http.Header
		expectHeaders http.Header
	}{{
		name: "prunes activator headers, does not add user agent header",
		url:  "http://example.com/",
		header: http.Header{
			"Header-Not-To-Remove": []string{"value"},
			"Header-To-Remove-1":   []string{"some-value"},
			"Header-To-Remove-2":   []string{"some-value"},
		},
		expectHeaders: http.Header{
			"Host":                 []string{"example.com"},
			"Header-Not-To-Remove": []string{"value"},
		},
	}, {
		name: "explicit user agent header not removed",
		url:  "http://example.com/",
		header: http.Header{
			netheader.UserAgentKey: []string{"gold"},
		},
		expectHeaders: http.Header{
			"Host":                 []string{"example.com"},
			netheader.UserAgentKey: []string{"gold"},
		},
	}, {
		name: "overrides host header",
		url:  "http://example.com/",
		host: "foo.bar",
		header: http.Header{
			netheader.UserAgentKey: []string{"gold"},
		},
		expectHeaders: http.Header{
			"Host":                                []string{"foo.bar"},
			netheader.PassthroughLoadbalancingKey: []string{"true"},
			netheader.UserAgentKey:                []string{"gold"},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proxy := NewHeaderPruningReverseProxy(serverURL.Host, test.host, []string{
				"header-to-remove-1",
				"header-to-remove-2",
			}, false /* use HTTPS */)

			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, test.url, nil)
			req.Header = test.header

			proxy.ServeHTTP(resp, req)

			var proxiedHeaders http.Header
			if err := json.NewDecoder(resp.Body).Decode(&proxiedHeaders); err != nil {
				t.Fatalf("Decode = %v", err)
			}

			// Remove headers golang adds from consideration.
			for _, k := range []string{"Accept-Encoding", "Content-Length", "X-Forwarded-For"} {
				proxiedHeaders.Del(k)
			}

			if got, want := proxiedHeaders, test.expectHeaders; !cmp.Equal(want, got) {
				t.Errorf("Got Headers=%v, want: %v; diff: %s", got, want, cmp.Diff(want, got))
			}
		})
	}
}

func TestNewHeaderPruningProxyHTTPS(t *testing.T) {
	var handler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		r.Header.Add("Host", r.Host) // Explicitly add the host header so we can assert.
		if err := json.NewEncoder(w).Encode(r.Header); err != nil {
			panic(err)
		}
	}

	server := httptest.NewTLSServer(handler)
	serverURL, _ := url.Parse(server.URL)
	defer server.Close()

	rootCAs := x509.NewCertPool()
	rootCAs.AddCert(server.Certificate())
	tlsConf := &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    rootCAs,
	}

	tests := []struct {
		name          string
		url           string
		host          string
		header        http.Header
		expectHeaders http.Header
	}{{
		name: "prunes activator headers, does not add user agent header",
		url:  "https://example.com/",
		header: http.Header{
			"Header-Not-To-Remove": []string{"value"},
			"Header-To-Remove-1":   []string{"some-value"},
			"Header-To-Remove-2":   []string{"some-value"},
		},
		expectHeaders: http.Header{
			"Host":                 []string{"example.com"},
			"Header-Not-To-Remove": []string{"value"},
		},
	}, {
		name: "explicit user agent header not removed",
		url:  "https://example.com/",
		header: http.Header{
			netheader.UserAgentKey: []string{"gold"},
		},
		expectHeaders: http.Header{
			"Host":                 []string{"example.com"},
			netheader.UserAgentKey: []string{"gold"},
		},
	}, {
		name: "overrides host header",
		url:  "https://example.com/",
		host: "foo.bar",
		header: http.Header{
			netheader.UserAgentKey: []string{"gold"},
		},
		expectHeaders: http.Header{
			"Host":                                []string{"foo.bar"},
			netheader.PassthroughLoadbalancingKey: []string{"true"},
			netheader.UserAgentKey:                []string{"gold"},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proxy := NewHeaderPruningReverseProxy(serverURL.Host, test.host, []string{
				"header-to-remove-1",
				"header-to-remove-2",
			}, true /* use HTTPS */)

			resp := httptest.NewRecorder()

			req := httptest.NewRequest(http.MethodPost, test.url, nil)
			req.Header = test.header

			proxy.Transport = &http.Transport{TLSClientConfig: tlsConf}
			proxy.ServeHTTP(resp, req)

			var proxiedHeaders http.Header
			if err := json.NewDecoder(resp.Body).Decode(&proxiedHeaders); err != nil {
				t.Fatalf("Decode = %v", err)
			}

			// Remove headers golang adds from consideration.
			for _, k := range []string{"Accept-Encoding", "Content-Length", "X-Forwarded-For"} {
				proxiedHeaders.Del(k)
			}

			if got, want := proxiedHeaders, test.expectHeaders; !cmp.Equal(want, got) {
				t.Errorf("Got Headers=%v, want: %v; diff: %s", got, want, cmp.Diff(want, got))
			}
		})
	}
}
