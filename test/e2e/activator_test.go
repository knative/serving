package e2e

import (
	"testing"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/test"

	pkgTest "github.com/knative/pkg/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"fmt"
	//"time"
	"net/http"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"time"
	"github.com/knative/pkg/test/spoof"
	"golang.org/x/sync/errgroup"
	"sync/atomic"
)

func TestServiceThroughput(t *testing.T) {
	var (
		clients *test.Clients
		logger  *logging.BaseLogger
	)
	// Create a service with concurrency 1 that could sleep for N ms.
	// Limit its maxScale to 10 containers, wait for the service to scale down and hit it with concurrent requests.
	logger = logging.GetContextLogger(t.Name())
	clients = Setup(t)

	helloWorldNames := test.ResourceNames{
		Config: test.AppendRandomString(configName, logger),
		Route:  test.AppendRandomString(routeName, logger),
		Image:  "observed-concurrency",
	}

	configOptions := test.Options{
		ContainerConcurrency: 1,
	}

	fopt := func(config *v1alpha1.Configuration) {
		config.Spec.RevisionTemplate.Annotations = make(map[string]string)
		config.Spec.RevisionTemplate.Annotations["autoscaling.knative.dev/maxScale"] = "10"
	}

	if _, err := test.CreateConfiguration(logger, clients, helloWorldNames, &configOptions, fopt); err != nil {
		t.Fatalf("Failed to create Configuration: %v", err)
	}

	if _, err := test.CreateRoute(logger, clients, helloWorldNames); err != nil {
		t.Fatalf("Failed to create Route: %v", err)
	}

	test.CleanupOnInterrupt(func() { TearDown(clients, helloWorldNames, logger) }, logger)
	defer TearDown(clients, helloWorldNames, logger)

	revision, err := test.WaitForConfigLatestRevision(clients, helloWorldNames)
	if err != nil {
		t.Fatalf("Configuration %s was not updated with the new revision: %v", helloWorldNames.Config, err)
	}

	logger.Info("When the Route reports as Ready, everything should be ready.")
	if err := test.WaitForRouteState(clients.ServingClient, helloWorldNames.Route, test.IsRouteReady, "RouteIsReady"); err != nil {
		t.Fatalf("The Route %s was not marked as Ready to serve traffic: %v", helloWorldNames.Route, err)
	}

	helloWorldRoute, err := clients.ServingClient.Routes.Get(helloWorldNames.Route, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Route %q of helloworld app: %v", helloWorldNames.Route, err)
	}

	domain := helloWorldRoute.Status.Domain

	deploymentName := revision + "-deployment"

	logger.Info("Waiting for deployment to scale to zero.")
	if err := pkgTest.WaitForDeploymentState(
		clients.KubeClient,
		deploymentName,
		test.DeploymentScaledToZeroFunc,
		"DeploymentScaledToZero",
		test.ServingNamespace,
		2*time.Minute); err != nil {
		fmt.Errorf("Failed waiting for deployment to scale to zero: %v", err)
	}

	// Hit the service with concurrent requests to provoke the start from 0,
	// the requests should go through activator, we make sure we receive only 200 responses.

	logger.Info("Waiting for endpoint to serve request")

	endpoint, err := spoof.GetServiceEndpoint(clients.KubeClient.Kube)
	url := fmt.Sprintf("http://%s/?timeout=100", *endpoint)

	client, err := pkgTest.NewSpoofingClient(clients.KubeClient, logger, domain, test.ServingFlags.ResolvableDomain)

	totalRequests := 1000
	responseChannel := make(chan *spoof.Response, totalRequests)
	timeout := 30 * time.Second
	roundTrip := roundTrip(client, url)

	sendRequests(roundTrip, 1000, totalRequests, responseChannel, timeout, logger, t)

	collectResponses(responseChannel, totalRequests, timeout, t)
}

func sendRequests(roundTrip func() (*spoof.Response, error), concurrency, totalRequests int, resChannel chan *spoof.Response, timeout time.Duration, logger *logging.BaseLogger, t *testing.T) int32 {
	var (
		group     errgroup.Group
		responses int32
	)
	// The eventual number of sent requests is mod(concurrency).
	runs := totalRequests / concurrency
	tickChan := make(chan struct{}, runs)
	doneChan := make(chan struct{})
	timeoutChan := time.After(timeout)
	errChan := make(chan error)
	allDoneChan := make(chan struct{})

	// Send a tick for each batch of requests.
	go func() {
		for i := 0; i < runs; i++ {
			tickChan <- struct{}{}
			<-doneChan
		}
		allDoneChan <- struct{}{}
	}()

	// Run a batch of requests.
	go func() {
		for {
			select {
			case <-tickChan:
				logger.Info("Starting to execute the batch of requests")
				//	Send requests async and wait for the responses.
				for i := 0; i < concurrency; i++ {
					group.Go(func() error {
						res, err := roundTrip()
						if err != nil {
							errChan <- fmt.Errorf("Unexpected error sending a request, %v\n", err)
						}
						atomic.AddInt32(&responses, 1)
						resChannel <- res
						return nil
					})
				}
				err := group.Wait()
				if err != nil {
					errChan <- fmt.Errorf("Unexpected error making requests against activator: %v", err)
				}
				logger.Info("Finished the execution of the requests")
				doneChan <- struct{}{}
				continue
			case <-timeoutChan:
				errChan <- fmt.Errorf("Timeout out waiting for responses, want - %d, got - %d", totalRequests, atomic.LoadInt32(&responses))
				return
			}
		}
	}()
	logger.Info("Waiting for all requests to finish")
	select {
	case <-allDoneChan:
		// success
	case err := <-errChan:
		t.Fatalf("Error happened while waiting for the responses: %v", err)
	case <-timeoutChan:
		t.Fatalf("Timeout waiting for resopnses")
	}

	logger.Info("All requests are finished")
	return atomic.LoadInt32(&responses)
}

func collectResponses(respChan chan *spoof.Response, total int, timeout time.Duration, t *testing.T) {
	timeoutChan := time.After(timeout)
	wantResponse := 200
	for i := 0; i < total; i++ {
		select {
		case resp := <-respChan:
			if resp != nil {
				if resp.StatusCode != wantResponse {
					t.Errorf("Response code expected: %d, got %d", wantResponse, resp.StatusCode)
				}
			} else {
				t.Errorf("No response code received for the request")
			}
		case <-timeoutChan:
			t.Errorf("Timed out waiting for responses after %s", timeout)
		}
	}
}

func roundTrip(client *spoof.SpoofingClient, url string) func() (*spoof.Response, error) {
	return func() (*spoof.Response, error) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("error creating http request: %v", err)
		}
		res, err := client.Do(req)
		return res, err
	}
}
