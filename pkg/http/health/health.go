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

package health

import (
	"fmt"
	"net/http"
	"strings"
)

type ProbeHandler struct {
	NextHandler http.Handler
}

func isProbe(r *http.Request) bool {
	fmt.Printf("kube-probe: %t\n", strings.HasPrefix(r.Header.Get("User-Agent"), "kube-probe/"))
	// Since K8s 1.8, prober requests have
	//   User-Agent = "kube-probe/{major-version}.{minor-version}".
	return strings.HasPrefix(r.Header.Get("User-Agent"), "kube-probe/")
}

func (rh *ProbeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isProbe(r) {
		w.WriteHeader(http.StatusOK)
		return
	}
	rh.NextHandler.ServeHTTP(w, r)
}
