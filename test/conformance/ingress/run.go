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

import "testing"

func RunConformance(t *testing.T) {
	t.Run("basics", TestBasics)
	t.Run("basics/http2", TestBasicsHTTP2)

	t.Run("grpc", TestGRPC)
	t.Run("grpc/split", TestGRPC)

	t.Run("headers/pre-split", TestPreSplitSetHeaders)
	t.Run("headers/post-split", TestPostSplitSetHeaders)

	t.Run("hosts/multiple", TestMultipleHosts)

	t.Run("dispatch/path", TestPath)
	t.Run("dispatch/percentage", TestPercentage)
	t.Run("dispatch/path_and_percentage", TestPathAndPercentageSplit)

	t.Run("retry", TestRetry)
	t.Run("timeout", TestTimeout)

	t.Run("tls", TestIngressTLS)
	t.Run("update", TestUpdate)

	t.Run("visibility", TestVisibility)
	t.Run("visibility/split", TestVisibilitySplit)
	t.Run("visibility/path", TestVisibilityPath)

	t.Run("websocket", TestWebsocket)
	t.Run("websocket/split", TestWebsocketSplit)
}
