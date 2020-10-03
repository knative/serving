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

package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	pkgTest "knative.dev/pkg/test"
	"knative.dev/serving/test"
	"knative.dev/serving/test/types"

	v1testing "knative.dev/serving/pkg/testing/v1"
	v1test "knative.dev/serving/test/v1"
)

// fetchRuntimeInfo creates a Service that uses the 'runtime' test image, and extracts the returned output into the
// RuntimeInfo object. The 'runtime' image uses uid 65532.
func fetchRuntimeInfo(
	t *testing.T,
	clients *test.Clients,
	opts ...interface{}) (*test.ResourceNames, *types.RuntimeInfo, error) {

	names := &test.ResourceNames{Image: test.Runtime}
	t.Helper()
	names.Service = test.ObjectNameForTest(t)

	test.EnsureTearDown(t, clients, names)

	serviceOpts, reqOpts, err := splitOpts(opts...)
	if err != nil {
		return nil, nil, err
	}

	objects, err := v1test.CreateServiceReady(t, clients, names,
		serviceOpts...)
	if err != nil {
		return nil, nil, err
	}

	resp, err := pkgTest.WaitForEndpointState(
		context.Background(),
		clients.KubeClient,
		t.Logf,
		objects.Service.Status.URL.URL(),
		v1test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"RuntimeInfo",
		test.ServingFlags.ResolvableDomain,
		append(reqOpts, test.AddRootCAtoTransport(context.Background(), t.Logf, clients, test.ServingFlags.HTTPS))...)
	if err != nil {
		return nil, nil, err
	}

	var ri types.RuntimeInfo
	err = json.Unmarshal(resp.Body, &ri)
	return names, &ri, err
}

func splitOpts(opts ...interface{}) ([]v1testing.ServiceOption, []interface{}, error) {
	serviceOpts := []v1testing.ServiceOption{}
	reqOpts := []interface{}{}
	for _, opt := range opts {
		switch t := opt.(type) {
		case v1testing.ServiceOption:
			serviceOpts = append(serviceOpts, opt.(v1testing.ServiceOption))
		case pkgTest.RequestOption:
			reqOpts = append(reqOpts, opt.(pkgTest.RequestOption))
		default:
			return nil, nil, fmt.Errorf("invalid option type: %T", t)
		}

	}
	return serviceOpts, reqOpts, nil
}
