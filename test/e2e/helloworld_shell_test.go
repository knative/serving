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
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/knative/serving/test"
	"go.uber.org/zap"
)

const (
	appYaml              = "../test_images/helloworld/helloworld.yaml"
	yamlImagePlaceholder = "github.com/knative/serving/test_images/helloworld"
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

func cleanup(yamlFilename string, logger *zap.SugaredLogger) {
	exec.Command("kubectl", "delete", "-f", yamlFilename).Run()
	os.Remove(yamlFilename)
	// There seems to be an Istio bug where if we delete / create
	// VirtualServices too quickly we will hit pro-longed "No health
	// upstream" causing timeouts.  Adding this small sleep to
	// sidestep the issue.
	//
	// TODO(#1376):  Fix this when upstream fix is released.
	logger.Info("Sleeping for 20 seconds after Route deletion to avoid hitting issue in #1376")
	time.Sleep(20 * time.Second)
}

func TestHelloWorldFromShell(t *testing.T) {
	// Add test case specific name to its own logger
	logger := test.Logger.Named("TestHelloWorldFromShell")

	imagePath := strings.Join([]string{test.Flags.DockerRepo, "helloworld"}, "/")

	logger.Infof("Creating manifest")

	// Create manifest file.
	newYaml, err := ioutil.TempFile("", "helloworld")
	if err != nil {
		t.Fatalf("Failed to create temporary manifest: %v", err)
	}
	newYamlFilename := newYaml.Name()
	defer cleanup(newYamlFilename, logger)
	test.CleanupOnInterrupt(func() { cleanup(newYamlFilename, logger) }, logger)

	// Populate manifets file with the real path to the container
	content, err := ioutil.ReadFile(appYaml)
	if err != nil {
		t.Fatalf("Failed to read file %s: %v", appYaml, err)
	}
	realContent := strings.Replace(string(content), yamlImagePlaceholder, imagePath, -1)
	if _, err = newYaml.WriteString(realContent); err != nil {
		t.Fatalf("Failed to write new manifest: %v", err)
	}
	if err = newYaml.Close(); err != nil {
		t.Fatalf("Failed to close new manifest file: %v", err)
	}

	logger.Infof("Manifest file is '%s'", newYamlFilename)
	logger.Info("Deploying using kubectl")

	// Deply using kubectl
	if err = exec.Command("kubectl", "apply", "-f", newYamlFilename).Run(); err != nil {
		t.Fatalf("Error running kubectl: %v", err)
	}

	logger.Info("Waiting for ingress to come up")

	// Wait for ingress to come up
	serviceIP := ""
	serviceHost := ""
	timeout := ingressTimeout
	for (serviceIP == "" || serviceHost == "") && timeout >= 0 {
		serviceHost = noStderrShell("kubectl", "get", "route", "route-example", "-o", "jsonpath={.status.domain}")
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
		output, err := exec.Command("curl", "--header", "Host:"+serviceHost, "http://"+serviceIP).Output()
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
