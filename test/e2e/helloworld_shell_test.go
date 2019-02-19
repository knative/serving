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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/test"
)

const (
	appYaml              = "../test_images/helloworld/helloworld.yaml"
	yamlImagePlaceholder = "github.com/knative/serving/test/test_images/helloworld"
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

func cleanup(yamlFilename string) {
	exec.Command("kubectl", "delete", "-f", yamlFilename).Run()
	os.Remove(yamlFilename)
}

func serviceHostname() string {
	return noStderrShell("kubectl", "get", "rt", "route-example", "-o", "jsonpath={.status.domain}", "-n", test.ServingNamespace)
}

func ingressAddress(gateway string, addressType string) string {
	return noStderrShell("kubectl", "get", "svc", gateway, "-n", "istio-system",
		"-o", fmt.Sprintf("jsonpath={.status.loadBalancer.ingress[*]['%s']}", addressType))
}

func TestHelloWorldFromShell(t *testing.T) {
	t.Parallel()
	//add test case specific name to its own logger
	logger := logging.GetContextLogger(t.Name())
	imagePath := test.ImagePath("helloworld")

	logger.Info("Creating manifest")

	// Create manifest file.
	newYaml, err := ioutil.TempFile("", "helloworld")
	if err != nil {
		t.Fatalf("Failed to create temporary manifest: %v", err)
	}
	newYamlFilename := newYaml.Name()
	defer cleanup(newYamlFilename)
	test.CleanupOnInterrupt(func() { cleanup(newYamlFilename) })

	// Populate manifets file with the real path to the container
	yamlBytes, err := ioutil.ReadFile(appYaml)

	if err != nil {
		t.Fatalf("Failed to read file %s: %v", appYaml, err)
	}

	content := strings.Replace(string(yamlBytes), yamlImagePlaceholder, imagePath, -1)
	content = strings.Replace(string(content), namespacePlaceholder, test.ServingNamespace, -1)

	if _, err = newYaml.WriteString(content); err != nil {
		t.Fatalf("Failed to write new manifest: %v", err)
	}
	if err = newYaml.Close(); err != nil {
		t.Fatalf("Failed to close new manifest file: %v", err)
	}

	logger.Infof("Manifest file is %q", newYamlFilename)
	logger.Info("Deploying using kubectl")

	// Deploy using kubectl
	if output, err := exec.Command("kubectl", "apply", "-f", newYamlFilename).CombinedOutput(); err != nil {
		t.Fatalf("Error running kubectl: %v", strings.TrimSpace(string(output)))
	}

	logger.Info("Waiting for ingress to come up")
	gateway := "istio-ingressgateway"
	// Wait for ingress to come up
	ingressAddr := ""
	serviceHost := ""
	timeout := ingressTimeout
	for (ingressAddr == "" || serviceHost == "") && timeout >= 0 {
		if serviceHost == "" {
			serviceHost = serviceHostname()
		}
		if ingressAddr = ingressAddress(gateway, "ip"); ingressAddr == "" {
			ingressAddr = ingressAddress(gateway, "hostname")
		}
		timeout -= checkInterval
		time.Sleep(checkInterval)
	}
	if ingressAddr == "" || serviceHost == "" {
		// serviceHost or ingressAddr might contain a useful error, dump them.
		t.Fatalf("Ingress not found (ingress='%s', host='%s')", ingressAddr, serviceHost)
	}
	logger.Infof("Curling %s/%s", ingressAddr, serviceHost)

	outputString := ""
	timeout = servingTimeout
	for outputString != helloWorldExpectedOutput && timeout >= 0 {
		var cmd *exec.Cmd
		if test.ServingFlags.ResolvableDomain {
			cmd = exec.Command("curl", serviceHost)
		} else {
			cmd = exec.Command("curl", "--header", "Host:"+serviceHost, "http://"+ingressAddr)
		}
		output, err := cmd.Output()
		errorString := "none"
		if err != nil {
			errorString = err.Error()
		}
		outputString = strings.TrimSpace(string(output))
		logger.Infof("App replied with '%s' (error: %s)", outputString, errorString)
		timeout -= checkInterval
		time.Sleep(checkInterval)
	}

	if outputString != helloWorldExpectedOutput {
		t.Fatal("Timeout waiting for app to start serving")
	}
	logger.Info("App is serving")
}
