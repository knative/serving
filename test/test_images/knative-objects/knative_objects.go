/*
Copyright 2018 The Knative Authors

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

package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	pkgTest "github.com/knative/pkg/test"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/test"
)

const (
	helloWorldExpectedOutput = "Hello World! How about some tasty noodles?"
)

// Setup creates the client objects needed in the e2e tests.
func setup() (*test.Clients, error) {
	clients, err := test.NewClients(
		pkgTest.Flags.Kubeconfig,
		pkgTest.Flags.Cluster,
		test.ServingNamespace)
	if err != nil {
		return nil, err
	}
	return clients, nil
}

// TearDown will delete created names using clients.
func tearDown(clients *test.Clients, names test.ResourceNames, logger *logging.BaseLogger) {
	if clients != nil && clients.ServingClient != nil {
		clients.ServingClient.Delete([]string{names.Route}, []string{names.Config}, []string{names.Service})
	}
}

func createServiceHandler(w http.ResponseWriter, r *http.Request) {
	logger := logging.GetContextLogger("CreateServiceHandler")

	clients, err := setup()
	if err != nil {
		fmt.Fprintf(w, "Cannot initialize clients: %v", err)
		return
	}

	names := test.ResourceNames{
		Service: test.AppendRandomString("CreateServiceHandler", logger),
		Image:   "helloworld",
	}

	defer tearDown(clients, names, logger)
	test.CleanupOnInterrupt(func() { tearDown(clients, names, logger) }, logger)

	fmt.Fprintf(w, "Creating a new service")
	objs, err := test.CreateRunLatestServiceReady(logger, clients, &names, &test.Options{})
	if err != nil {
		fmt.Fprintf(w, "Failed to create Service: %v", err)
		return
	}

	domain := objs.Route.Status.Domain
	_, err = pkgTest.WaitForEndpointState(
		clients.KubeClient,
		logger,
		domain,
		pkgTest.Retrying(pkgTest.EventuallyMatchesBody(helloWorldExpectedOutput), http.StatusNotFound),
		"WaitForEndpointToServeText",
		test.ServingFlags.ResolvableDomain)
	if err != nil {
		fmt.Fprintf(w, "The endpoint for created Service %s at domain %s didn't serve the expected text %q: %v", names.Service, domain, helloWorldExpectedOutput, err)
	} else {
		fmt.Fprintf(w, "%s is ready.", names.Service)
	}
}

func main() {
	flag.Parse()
	log.Print("Knative objects test app started.")
	test.ListenAndServeGracefullyWithPattern(fmt.Sprintf(":%d", test.EnvImageServerPort), map[string]func(w http.ResponseWriter, r *http.Request){
		test.KnativeObjectsCreateServicePath: createServiceHandler,
	})
}
