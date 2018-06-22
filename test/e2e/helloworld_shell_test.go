// +build e2e

/*
Copyright 2018 Google Inc. All Rights Reserved.
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

	"github.com/golang/glog"
	"github.com/knative/serving/test"
)

const (
	helloWorldSampleExpectedOutput = "Hello World: shiniestnewestversion!"
	sampleYaml = "../../sample/helloworld/sample.yaml"
	yamlImagePlaceholder = "github.com/knative/serving/sample/helloworld"
	ingressTimeout = 5 * time.Minute
	servingTimeout = 2 * time.Minute
	checkInterval = 2 * time.Second
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

func TestHelloWorldFromShell(t *testing.T) {
	// Container is expected to live in <gcr>/sample/helloworld
	// To regenerate it, see https://github.com/knative/serving/tree/master/sample/helloworld#setup
	imagePath := strings.Join([]string{test.Flags.DockerRepo, "sample", "helloworld"}, "/")

	glog.Infof("Creating manifest")

	// Create manifest file.
	newYaml, err := ioutil.TempFile("", "helloworld")
	if err != nil {
		t.Fatalf("Failed to create temporary manifest: %v", err)
	}
	newYamlFilename := newYaml.Name()
	defer cleanup(newYamlFilename)
	test.CleanupOnInterrupt(func() { cleanup(newYamlFilename) })

	// Populate manifets file with the real path to the container
	content, err := ioutil.ReadFile(sampleYaml)
	if err != nil {
		t.Fatalf("Failed to read file %s: %v", sampleYaml, err)
	}
	realContent := strings.Replace(string(content), yamlImagePlaceholder, imagePath, -1)
	if _, err = newYaml.WriteString(realContent); err != nil {
		t.Fatalf("Failed to write new manifest: %v", err)
	}
	if err = newYaml.Close(); err != nil {
		t.Fatalf("Failed to close new manifest file: %v", err)
	}

	glog.Infof("Manifest file is '%s'", newYamlFilename)
	glog.Info("Deploying using kubectl")

	// Deply using kubectl
	if err = exec.Command("kubectl", "apply", "-f", newYamlFilename).Run(); err != nil {
		t.Fatalf("Error running kubectl: %v", err)
	}

	glog.Info("Waiting for ingress to come up")

	// Wait for ingress to come up
	serviceIP := ""
	serviceHost := ""
	timeout := ingressTimeout
	for (serviceIP == "" || serviceHost == "") && timeout >= 0 {
		serviceHost = noStderrShell("kubectl", "get", "route", "route-example", "-o", "jsonpath={.status.domain}")
		serviceIP = noStderrShell("kubectl", "get", "ingress", "route-example-ingress", "-o", "jsonpath={.status.loadBalancer.ingress[*]['ip']}")
		time.Sleep(checkInterval)
		timeout = timeout - checkInterval
	}
	if (serviceIP == "" || serviceHost == "") {
		// serviceHost or serviceIP might contain a useful error, dump them.
		t.Fatalf("Ingress not found (IP='%s', host='%s')", serviceIP, serviceHost)
	}
	glog.Infof("Ingress is at %s/%s", serviceIP, serviceHost)

	glog.Info("Accessing app using curl")

	outputString := ""
	timeout = servingTimeout
	for (outputString != helloWorldSampleExpectedOutput && timeout >= 0) {
		output, err := exec.Command("curl", "--header", "Host:" + serviceHost, "http://" + serviceIP).Output()
		errorString := "none"
		time.Sleep(checkInterval)
		timeout = timeout - checkInterval
		if err != nil {
			errorString = err.Error()
		}
		outputString = strings.TrimSpace(string(output))
		glog.Infof("App replied with '%s' (error: %s)", outputString, errorString)
	}

	if outputString != helloWorldSampleExpectedOutput {
		t.Fatalf("Timeout waiting for app to start serving")
	}
	glog.Info("App is serving")
}
