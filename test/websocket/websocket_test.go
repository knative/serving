// +build e2e

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

package websocket

import (
	"net/http"
	"testing"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/test"
	corev1 "k8s.io/api/core/v1"

	// Mysteriously required to support GCP auth (required by k8s
	// libs). Apparently just importing it is enough. @_@ side effects
	// @_@. https://github.com/kubernetes/client-go/issues/242
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const (
	websocketServer = "wsserver"
	websocketClient = "wsclient"
)

func setup(t *testing.T) *test.Clients {
	t.Helper()
	clients, err := test.NewClients(pkgTest.Flags.Kubeconfig, pkgTest.Flags.Cluster, test.ServingNamespace)
	if err != nil {
		t.Fatalf("Couldn't initialize clients: %v", err)
	}
	return clients
}

func tearDown(clients *test.Clients, names test.ResourceNames) {
	if clients != nil && clients.ServingClient != nil {
		clients.ServingClient.Delete([]string{names.Route}, []string{names.Config}, []string{names.Service})
	}
}

func TestWebsocketConnection(t *testing.T) {
	clients := setup(t)

	// Add test case specific name to its own logger.
	logger := logging.GetContextLogger(t.Name())

	// Create the server.
	server_options := &test.Options{
		Labels: map[string]string{
			"serving.knative.dev/visibility": "cluster-local",
		},
	}
	server_names := test.ResourceNames{
		Service: test.AppendRandomString("ws-server-", logger),
		Image:   websocketServer,
	}
	// Must clean up in both abnormal and normal exit.
	defer tearDown(clients, server_names)
	test.CleanupOnInterrupt(func() { tearDown(clients, server_names) }, logger)
	if _, err := test.CreateRunLatestServiceReady(logger, clients, &server_names, server_options); err != nil {
		t.Fatalf("Failed to create websocket server: %v", err)
	}

	// Create the client.
	client_options := &test.Options{
		EnvVars: []corev1.EnvVar{{
			Name:  "TARGET",
			Value: server_names.Domain,
		}, {
			Name:  "HOST_HEADER",
			Value: server_names.Domain,
		}},
	}
	client_names := test.ResourceNames{
		Service: test.AppendRandomString("ws-client-", logger),
		Image:   websocketClient,
	}
	// Must clean up in both abnormal and normal exit.
	defer tearDown(clients, client_names)
	test.CleanupOnInterrupt(func() { tearDown(clients, client_names) }, logger)
	if _, err := test.CreateRunLatestServiceReady(logger, clients, &client_names, client_options); err != nil {
		t.Fatalf("Failed to create websocket client: %v", err)
	}

	if _, err := pkgTest.WaitForEndpointState(
		clients.KubeClient,
		logger,
		client_names.Domain,
		pkgTest.Retrying(pkgTest.MatchesBody("Hello"), http.StatusNotFound, http.StatusInternalServerError),
		"WebsocketClientServesText",
		test.ServingFlags.ResolvableDomain); err != nil {
		t.Fatalf("Fail to validate websocket connection %v: %v", server_names.Service, err)
	}
}
