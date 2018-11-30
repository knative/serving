// +build performance

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

package prometheus

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/knative/pkg/test/logging"
	"github.com/knative/serving/test"
	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

const (
	prometheusPort = "9090"
	appLabel       = "app=prometheus"
)

// PromProxy defines a proxy to the prometheus server
type PromProxy struct {
	Namespace      string
	portFwdProcess *os.Process
}

// Setup performs a port forwarding for app prometheus-test in given namespace
func (p *PromProxy) Setup(ctx context.Context, logger *logging.BaseLogger) error {
	return p.portForward(ctx, logger, appLabel, prometheusPort, prometheusPort)
}

// Kill the port forwarding process if running
func (p *PromProxy) Teardown(logger *logging.BaseLogger) error {
	logger.Info("Cleaning up prom proxy")
	if p.portFwdProcess != nil {
		return p.portFwdProcess.Kill()
	}
	return nil
}

// PortForward sets up local port forward to the pod specified by the "app" label in the given namespace
func (p *PromProxy) portForward(ctx context.Context, logger *logging.BaseLogger, labelSelector, localPort, remotePort string) error {
	var pod string
	var err error

	getName := fmt.Sprintf("kubectl -n %s get pod -l %s -o jsonpath='{.items[0].metadata.name}'", p.Namespace, labelSelector)
	pod, err = p.execShellCmd(ctx, logger, getName)
	if err != nil {
		return err
	}
	logger.Infof("%s pod name: %s", labelSelector, pod)

	logger.Infof("Setting up %s proxy", labelSelector)
	portFwdCmd := fmt.Sprintf("kubectl port-forward %s %s:%s -n %s", strings.Trim(pod, "'"), localPort, remotePort, p.Namespace)
	if p.portFwdProcess, err = p.executeCmdBackground(logger, portFwdCmd); err != nil {
		logger.Errorf("Failed to port forward: %s", err)
		return err
	}
	logger.Infof("running %s port-forward in background, pid = %d", labelSelector, p.portFwdProcess.Pid)
	return nil
}

// RunBackground starts a background process and returns the Process if succeed
func (p *PromProxy) executeCmdBackground(logger *logging.BaseLogger, format string, args ...interface{}) (*os.Process, error) {
	cmd := fmt.Sprintf(format, args...)
	logger.Infof("Executing command: %s", cmd)
	parts := strings.Split(cmd, " ")
	c := exec.Command(parts[0], parts[1:]...) // #nosec
	err := c.Start()
	if err != nil {
		logger.Errorf("%s, command failed!", cmd)
		return nil, err
	}
	return c.Process, nil
}

// ExecuteShellCmd executes a shell command
func (p *PromProxy) execShellCmd(ctx context.Context, logger *logging.BaseLogger, format string, args ...interface{}) (string, error) {
	cmd := fmt.Sprintf(format, args...)
	logger.Infof("Executing command: %s", cmd)
	c := exec.CommandContext(ctx, "sh", "-c", cmd) // #nosec
	bytes, err := c.CombinedOutput()
	if err != nil {
		logger.Infof("Command error: %v", err)
		return string(bytes), fmt.Errorf("command failed: %q %v", string(bytes), err)
	}

	if output := strings.TrimSuffix(string(bytes), "\n"); len(output) > 0 {
		logger.Infof("Command output: \n%s", output)
	}

	return string(bytes), nil
}

// PromAPI gets a handle to the prometheus API
func PromAPI() (v1.API, error) {
	client, err := api.NewClient(api.Config{Address: fmt.Sprintf("http://localhost:%s", prometheusPort)})
	if err != nil {
		return nil, err
	}
	return v1.NewAPI(client), nil
}

// AllowPrometheusSync sleeps for sometime to allow prometheus time to scrape the metrics.
func AllowPrometheusSync(logger *logging.BaseLogger) {
	logger.Info("Sleeping to allow prometheus to record metrics...")
	time.Sleep(30 * time.Second)
}

// RunQuery runs a prometheus query and returns the metric value
func RunQuery(ctx context.Context, logger *logging.BaseLogger, promAPI v1.API, metric string, names test.ResourceNames) float64 {
	query := fmt.Sprintf("%s{configuration_namespace=\"%s\", configuration=\"%s\", revision=\"%s\"}", metric, test.ServingNamespace, names.Config, names.Revision)
	logger.Infof("Prometheus query: %s", query)

	value, err := promAPI.Query(ctx, query, time.Now())
	if err != nil {
		logger.Errorf("Could not get metrics from prometheus: %v", err)
	}

	return VectorValue(value)
}

// VectorValue gets the vector value from the value type
func VectorValue(val model.Value) float64 {
	if val.Type() != model.ValVector {
		return 0
	}
	value := val.(model.Vector)
	return float64(value[0].Value)
}
