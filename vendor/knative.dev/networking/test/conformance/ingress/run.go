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

package ingress

import (
	"strings"
	"testing"

	"knative.dev/networking/test"
)

var stableTests = map[string]func(t *testing.T){
	"basics":                       TestBasics,
	"basics/http2":                 TestBasicsHTTP2,
	"grpc":                         TestGRPC,
	"grpc/split":                   TestGRPCSplit,
	"headers/pre-split":            TestPreSplitSetHeaders,
	"headers/post-split":           TestPostSplitSetHeaders,
	"hosts/multiple":               TestMultipleHosts,
	"dispatch/path":                TestPath,
	"dispatch/percentage":          TestPercentage,
	"dispatch/path_and_percentage": TestPathAndPercentageSplit,
	"timeout":                      TestTimeout,
	"tls":                          TestIngressTLS,
	"update":                       TestUpdate,
	"visibility":                   TestVisibility,
	"visibility/split":             TestVisibilitySplit,
	"visibility/path":              TestVisibilityPath,
	"ingressclass":                 TestIngressClass,
	"websocket":                    TestWebsocket,
	"websocket/split":              TestWebsocketSplit,
}

var betaTests = map[string]func(t *testing.T){
	// Add your conformance test for beta features
	"headers/probe": TestProbeHeaders,
}

var alphaTests = map[string]func(t *testing.T){
	// Add your conformance test for alpha features
	"headers/tags": TestTagHeaders,
	"host-rewrite": TestRewriteHost,
}

// RunConformance will run ingress conformance tests
//
// Depending on the options it may test alpha and beta features
func RunConformance(t *testing.T) {

	for name, test := range stableTests {
		t.Run(name, test)
	}

	skipTests := skipTests()

	// TODO(dprotaso) we'll need something more robust
	// in the long term that lets downstream
	// implementations to better select which tests
	// should be run -  selection across various
	// dimensions
	// ie. state - alpha, beta, ga
	// ie. requirement - must, should, may
	if test.ServingFlags.EnableBetaFeatures {
		for name, test := range betaTests {
			if _, ok := skipTests[name]; ok {
				t.Run(name, skipFunc)
				continue
			}
			t.Run(name, test)
		}
	}

	if test.ServingFlags.EnableAlphaFeatures {
		for name, test := range alphaTests {
			if _, ok := skipTests[name]; ok {
				t.Run(name, skipFunc)
				continue
			}
			t.Run(name, test)
		}
	}
}

var skipFunc = func(t *testing.T) {
	t.Skip("Skipping the test in skip-test flag")
}

func skipTests() map[string]struct{} {
	skipArray := strings.Split(test.ServingFlags.SkipTests, ",")
	skipMap := make(map[string]struct{}, len(skipArray))
	for _, name := range skipArray {
		skipMap[name] = struct{}{}
	}
	return skipMap
}
