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

package conformance

import (
	"bytes"
	"fmt"
	"net/url"
	"os/exec"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	connectRetryInterval = 1 * time.Second
	connectTimeout       = 20 * time.Second
)

// connect attempts to establish websocket connection with the Service.
// It will retry until reaching `connectTimeout` duration.
func connect(logger *logging.BaseLogger, ingressIP string, domain string) (*websocket.Conn, error) {
	u := url.URL{Scheme: "ws", Host: ingressIP, Path: "/"}
	var conn *websocket.Conn
	waitErr := wait.PollImmediate(connectRetryInterval, connectTimeout, func() (bool, error) {
		logger.Infof("Connecting using websocket: url=%s, host=%s", u.String(), domain)
		c, resp, err := websocket.DefaultDialer.Dial(u.String(), map[string][]string{"Host": []string{domain}})
		if err == nil {
			logger.Info("Connection established.")
			conn = c
			return true, nil
		}
		if resp != nil {
			body := new(bytes.Buffer)
			body.ReadFrom(resp.Body)
			logger.Infof("Connection failed: %v.  Response=%+v, ResponseBody=%q", err, resp, body)
		}
		return false, nil
	})
	return conn, waitErr
}

// While we do have similar logic in knative/pkg, it is deeply buried inside
// the SpoofClient which is very HTTP centric.
//
// TODO(tcnghia): Extract the GatewayIP logic out from SpoofClient, and reuse
// that here.
func getGatewayIP(kube *kubernetes.Clientset) (string, error) {
	ingressName := "istio-ingressgateway"
	ingressNamespace := "istio-system"

	ingress, err := kube.CoreV1().Services(ingressNamespace).Get(ingressName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if len(ingress.Status.LoadBalancer.Ingress) != 1 {
		return "", fmt.Errorf("Expected exactly one ingress load balancer, instead had %d: %s", len(ingress.Status.LoadBalancer.Ingress), ingress.Status.LoadBalancer.Ingress)
	}
	if ingress.Status.LoadBalancer.Ingress[0].IP == "" {
		return "", fmt.Errorf("Expected ingress loadbalancer IP for %s to be set, instead was empty", ingressName)
	}
	return ingress.Status.LoadBalancer.Ingress[0].IP, nil
}

func validateWebsocketConnection(logger *logging.BaseLogger, clients *test.Clients, names test.ResourceNames) error {
	// Get the service domain.
	svc, err := clients.ServingClient.Services.Get(names.Service, metav1.GetOptions{})
	if err != nil {
		return err
	}
	domain := svc.Status.Domain
	// Get the gatewayIP.
	gatewayIP, err := getGatewayIP(clients.KubeClient.Kube)
	if err != nil {
		return err
	}
	// Establish the websocket connection.
	conn, err := connect(logger, gatewayIP, domain)
	if err != nil {
		return err
	}
	defer conn.Close()
	// Send a message.
	sent := "Hello, websocket"
	logger.Infof("Sending message %q to server.", sent)
	if err = conn.WriteMessage(websocket.TextMessage, []byte(sent)); err != nil {
		return err
	}
	// Attempt to read back the same message.
	if _, recv, err := conn.ReadMessage(); err != nil {
		return err
	} else if sent != string(recv) {
		return fmt.Errorf("Expected to receive back the message: %q but received %q", sent, string(recv))
	} else {
		logger.Infof("Received message %q from echo server.", recv)
	}
	return nil
}

// TestWebsocket (1) creates a service based on the `websocketEcho` image,
// (2) connects to the service using websocket, (3) sends a message, and
// (4) verifies that we receive back the same message.
func TestWebsocket(t *testing.T) {
	clients := setup(t)

	// Add test case specific name to its own logger.
	logger := logging.GetContextLogger(t.Name())

	names := test.ResourceNames{
		Service: test.AppendRandomString("test-websocket-", logger),
		Image:   websocketEcho,
	}

	// Clean up on test failure or interrupt
	defer tearDown(clients, names)
	test.CleanupOnInterrupt(func() { tearDown(clients, names) }, logger)

	// Setup Service
	_, err := test.CreateRunLatestServiceReady(logger, clients, &names, &test.Options{})
	if err != nil {
		t.Fatalf("Failed to create initial Service %v: %v", names.Service, err)
	}

	// Validate the websocket connection.
	err = validateWebsocketConnection(logger, clients, names)
	printAllLogs(logger, clients, names)
	if err != nil {
		t.Error(err)
	}
}

func combineShell(logger *logging.BaseLogger, name string, arg ...string) string {
	cmd := exec.Command(name, arg...)
	out, _ := cmd.CombinedOutput()
	return string(out)
}

func printPodLogs(logger *logging.BaseLogger, ns string, key string, value string, containers []string) {
	pods := combineShell(logger, "kubectl", "get", "pods", "-n", ns, "-l", key+"="+value)
	logger.Infof("PODS LIST [%s=%s] ------\n%s", key, value, pods)
	logger.Infof("PODS LIST [%s=%s] ------\n", key, value)
	for _, c := range containers {
		logs := combineShell(logger, "kubectl", "logs", "-n", ns, "-l", key+"="+value, "-c", c)
		logger.Infof("LOG START   [%s] ------\n%s", c, logs)
		logger.Infof("POD LOG END [%s] ------\n", c)
	}
}

func printAllLogs(logger *logging.BaseLogger, clients *test.Clients, names test.ResourceNames) {
	printPodLogs(logger, "serving-tests", "serving.knative.dev/service", names.Service,
		[]string{"queue-proxy", "user-container"})
	printPodLogs(logger, "knative-serving", "app", "activator", []string{"activator"})
}
