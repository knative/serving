/*
Copyright 2022 The Knative Authors

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

import "net/http"

// IsPotentialMeshErrorResponse returns whether the HTTP response is compatible
// with having been caused by attempting direct connection when mesh was
// enabled. For example if we get a HTTP 404 status code it's safe to assume
// mesh is not enabled even if a probe was otherwise unsuccessful. This is
// useful to avoid falling back to ClusterIP when we see errors which are
// unrelated to mesh being enabled.
func IsPotentialMeshErrorResponse(resp *http.Response) bool {
	return resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusBadGateway
}
