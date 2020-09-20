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
	"testing"

	"knative.dev/networking/test"
)

// RunConformance will run ingress conformance tests
//
// Depending on the options it may test alpha and beta features
func RunConformance(t *testing.T) {
	t.Run("basics", TestBasics)
	t.Run("basics/http2", TestBasicsHTTP2)

	t.Run("grpc", TestGRPC)
	t.Run("grpc/split", TestGRPCSplit)

	t.Run("headers/pre-split", TestPreSplitSetHeaders)
	t.Run("headers/post-split", TestPostSplitSetHeaders)

	t.Run("hosts/multiple", TestMultipleHosts)

	t.Run("dispatch/path", TestPath)
	t.Run("dispatch/percentage", TestPercentage)
	t.Run("dispatch/path_and_percentage", TestPathAndPercentageSplit)

	t.Run("timeout", TestTimeout)

	t.Run("tls", TestIngressTLS)
	t.Run("update", TestUpdate)

	t.Run("visibility", TestVisibility)
	t.Run("visibility/split", TestVisibilitySplit)
	t.Run("visibility/path", TestVisibilityPath)

	t.Run("ingressclass", TestIngressClass)

	t.Run("websocket", TestWebsocket)
	t.Run("websocket/split", TestWebsocketSplit)

	// TODO(dprotaso) we'll need something more robust
	// in the long term that lets downstream
	// implementations to better select which tests
	// should be run -  selection across various
	// dimensions
	// ie. state - alpha, beta, ga
	// ie. requirement - must, should, may

	if test.ServingFlags.EnableBetaFeatures {
		// Add your conformance test for beta features
		t.Run("headers/probe", TestProbeHeaders)
	}

	if test.ServingFlags.EnableAlphaFeatures {
		// Add your conformance test for alpha features
		t.Run("headers/tags", TestTagHeaders)
		t.Run("host-rewrite", TestRewriteHost)
	}
}
