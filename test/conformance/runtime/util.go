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
	"encoding/json"
	"fmt"
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/serving/test"
	"github.com/knative/serving/test/types"
	v1a1test "github.com/knative/serving/test/v1alpha1"

	. "github.com/knative/serving/pkg/testing/v1alpha1"
)

// fetchRuntimeInfoUnprivileged creates a Service that uses the 'runtime-unprivileged' test image, and extracts the returned output into the
// RuntimeInfo object.
func fetchRuntimeInfoUnprivileged(
	t *testing.T,
	clients *test.Clients,
	opts ...interface{}) (*test.ResourceNames, *types.RuntimeInfo, error) {

	return runtimeInfo(t, clients, &test.ResourceNames{Image: test.RuntimeUnprivileged}, opts...)
}

// fetchRuntimeInfo creates a Service that uses the 'runtime' test image, and extracts the returned output into the
// RuntimeInfo object. The 'runtime' image uses uid 0.
func fetchRuntimeInfo(
	t *testing.T,
	clients *test.Clients,
	opts ...interface{}) (*test.ResourceNames, *types.RuntimeInfo, error) {

	return runtimeInfo(t, clients, &test.ResourceNames{}, opts...)
}

func runtimeInfo(
	t *testing.T,
	clients *test.Clients,
	names *test.ResourceNames,
	opts ...interface{}) (*test.ResourceNames, *types.RuntimeInfo, error) {

	t.Helper()
	names.Service = test.ObjectNameForTest(t)
	if names.Image == "" {
		names.Image = test.Runtime
	} else if names.Image != test.RuntimeUnprivileged {
		return nil, nil, fmt.Errorf("invalid image provided: %s", names.Image)
	}

	defer test.TearDown(clients, *names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, *names) })

	serviceOpts, reqOpts, err := splitOpts(opts...)
	if err != nil {
		return nil, nil, err
	}

	objects, err := v1a1test.CreateRunLatestServiceReady(t, clients, names, &v1a1test.Options{}, serviceOpts...)
	if err != nil {
		return nil, nil, err
	}

	resp, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		objects.Service.Status.URL.Host,
		v1a1test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"RuntimeInfo",
		test.ServingFlags.ResolvableDomain,
		reqOpts...)
	if err != nil {
		return nil, nil, err
	}

	var ri types.RuntimeInfo
	err = json.Unmarshal(resp.Body, &ri)
	return names, &ri, err
}

func splitOpts(opts ...interface{}) ([]ServiceOption, []pkgTest.RequestOption, error) {
	serviceOpts := []ServiceOption{}
	reqOpts := []pkgTest.RequestOption{}
	for _, opt := range opts {
		switch t := opt.(type) {
		case ServiceOption:
			serviceOpts = append(serviceOpts, opt.(ServiceOption))
		case pkgTest.RequestOption:
			reqOpts = append(reqOpts, opt.(pkgTest.RequestOption))
		default:
			return nil, nil, fmt.Errorf("invalid option type: %T", t)
		}

	}
	return serviceOpts, reqOpts, nil
}
