/*
Copyright 2020 The Knative Authors

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

package network

import (
	"net/http"
	"testing"
)

func TestIsKubeletProbe(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatal("Error building request:", err)
	}
	if IsKubeletProbe(req) {
		t.Error("Not a kubelet probe but counted as such")
	}
	req.Header.Set("User-Agent", KubeProbeUAPrefix+"1.14")
	if !IsKubeletProbe(req) {
		t.Error("kubelet probe but not counted as such")
	}
	req.Header.Del("User-Agent")
	if IsKubeletProbe(req) {
		t.Error("Not a kubelet probe but counted as such")
	}
	req.Header.Set(KubeletProbeHeaderName, "no matter")
	if !IsKubeletProbe(req) {
		t.Error("kubelet probe but not counted as such")
	}
}
