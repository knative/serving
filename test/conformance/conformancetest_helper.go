// +build e2e

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

//runtime_conformance_helper.go contains helper methods used by conformance tests that verify runtime-contract.

package conformance

import (
	"encoding/json"
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/serving/test"
	"github.com/knative/serving/test/types"

	. "github.com/knative/serving/pkg/reconciler/testing"
)

// fetchRuntimeInfo creates a Service that uses the 'runtime' test image, and extracts the returned output into the
// RuntimeInfo object.
func fetchRuntimeInfo(t *testing.T, clients *test.Clients, options *test.Options, opts ...ServiceOption) (*test.ResourceNames, *types.RuntimeInfo, error) {
	names := test.ResourceNames{
		Service: test.ObjectNameForTest(t),
		Image:   runtime,
	}

	defer test.TearDown(clients, names)
	test.CleanupOnInterrupt(func() { test.TearDown(clients, names) })

	objects, err := test.CreateRunLatestServiceReady(t, clients, &names, options, opts...)
	if err != nil {
		return nil, nil, err
	}

	resp, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		t.Logf,
		objects.Service.Status.URL.Host,
		test.RetryingRouteInconsistency(pkgTest.IsStatusOK),
		"RuntimeInfo",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		return nil, nil, err
	}

	var ri types.RuntimeInfo
	err = json.Unmarshal(resp.Body, &ri)
	if err != nil {
		return nil, nil, err
	}
	return &names, &ri, nil
}
