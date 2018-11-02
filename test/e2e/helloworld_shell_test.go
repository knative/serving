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

package e2e

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/test"
)

const (
	appYaml              = "../test_images/helloworld/helloworld.yaml"
	yamlImagePlaceholder = "github.com/knative/serving/test_images/helloworld"
	namespacePlaceholder = "default"
	ingressTimeout       = 5 * time.Minute
	servingTimeout       = 2 * time.Minute
	checkInterval        = 2 * time.Second
)

func noStderrShell(name string, arg ...string) string {
	var void bytes.Buffer
	cmd := exec.Command(name, arg...)
	cmd.Stderr = &void
	out, _ := cmd.Output()
	return string(out)
}

func TestHelloWorldFromShell(t *testing.T) {
	//add test case specific name to its own logger
	logger := logging.GetContextLogger("TestHelloWorldFromShell")

	newYamlFilename, names, err := CreateConfigAndRouteFromYaml(logger,
		"helloworld",
		appYaml,
		"configuration-example",
		"route-example",
		yamlImagePlaceholder,
		namespacePlaceholder,
	)
	if err != nil {
		t.Fatalf("Failed to create configuration and route: %v", err)
	}

	defer TearDownByYaml(newYamlFilename, logger)
	test.CleanupOnInterrupt(func() { TearDownByYaml(newYamlFilename, logger) }, logger)

	logger.Info("Waiting for ingress to come up")

	// Wait for ingress to come up
	serviceIP := ""
	serviceHost := ""
	timeout := ingressTimeout
	for (serviceIP == "" || serviceHost == "") && timeout >= 0 {
		serviceHost = noStderrShell("kubectl", "get", "rt", names.Route, "-o", "jsonpath={.status.domain}", "-n", test.ServingNamespace)
		serviceIP = noStderrShell("kubectl", "get", "svc", "knative-ingressgateway", "-n", "istio-system",
			"-o", "jsonpath={.status.loadBalancer.ingress[*]['ip']}")
		time.Sleep(checkInterval)
		timeout = timeout - checkInterval
	}
	if serviceIP == "" || serviceHost == "" {
		// serviceHost or serviceIP might contain a useful error, dump them.
		t.Fatalf("Ingress not found (IP='%s', host='%s')", serviceIP, serviceHost)
	}
	logger.Infof("Ingress is at %s/%s", serviceIP, serviceHost)

	logger.Info("Accessing app using curl")

	outputString := ""
	timeout = servingTimeout
	for outputString != helloWorldExpectedOutput && timeout >= 0 {
		var cmd *exec.Cmd
		if test.ServingFlags.ResolvableDomain {
			cmd = exec.Command("curl", serviceHost)
		} else {
			cmd = exec.Command("curl", "--header", "Host:"+serviceHost, "http://"+serviceIP)
		}
		output, err := cmd.Output()
		errorString := "none"
		time.Sleep(checkInterval)
		timeout = timeout - checkInterval
		if err != nil {
			errorString = err.Error()
		}
		outputString = strings.TrimSpace(string(output))
		logger.Infof("App replied with '%s' (error: %s)", outputString, errorString)
	}

	if outputString != helloWorldExpectedOutput {
		t.Fatalf("Timeout waiting for app to start serving")
	}
	logger.Info("App is serving")
}
