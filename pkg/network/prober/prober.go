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

package prober

import (
	"context"
	"io/ioutil"
	"net/http"

	"github.com/pkg/errors"

	"github.com/knative/serving/pkg/network"
)

// Do sends a single probe to given target, e.g. `http://revision.default.svc.cluster.local:81`.
// headerValue is the value for the `k-network-probe` header.
// Do returns the status code, response body, and the request error, if any.
func Do(ctx context.Context, target, headerValue string) (int, string, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return 0, "", errors.Wrapf(err, "%s is not a valid URL", target)
	}

	req.Header.Set(http.CanonicalHeaderKey(network.ProbeHeaderName), headerValue)
	req = req.WithContext(ctx)
	resp, err := network.AutoTransport.RoundTrip(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, "", err
	}
	return resp.StatusCode, string(body), nil
}
