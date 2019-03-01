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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/knative/pkg/test/logging"
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
func (p *PromProxy) Setup(ctx context.Context, logf logging.FormatLogger) error {
	return p.portForward(ctx, logf, appLabel, prometheusPort, prometheusPort)
}

// Teardown will kill the port forwarding process if running.
func (p *PromProxy) Teardown() error {
	if p.portFwdProcess != nil {
		return p.portFwdProcess.Kill()
	}
	return nil
}

// PortForward sets up local port forward to the pod specified by the "app" label in the given namespace
func (p *PromProxy) portForward(ctx context.Context, logf logging.FormatLogger, labelSelector, localPort, remotePort string) error {
	getName := fmt.Sprintf("kubectl -n %s get pod -l %s -o jsonpath='{.items[0].metadata.name}'", p.Namespace, labelSelector)
	pod, err := p.execShellCmd(ctx, logf, getName)
	if err != nil {
		return err
	}

	logf("%s pod name: %s", labelSelector, pod)
	logf("Setting up %s proxy", labelSelector)

	portFwdCmd := fmt.Sprintf("kubectl port-forward %s %s:%s -n %s", strings.Trim(pod, "'"), localPort, remotePort, p.Namespace)
	if p.portFwdProcess, err = p.executeCmdBackground(logf, portFwdCmd); err != nil {
		return fmt.Errorf("Failed to port forward: %v", err)
	}
	logf("running %s port-forward in background, pid = %d", labelSelector, p.portFwdProcess.Pid)
	return nil
}

// RunBackground starts a background process and returns the Process if succeed
func (p *PromProxy) executeCmdBackground(logf logging.FormatLogger, format string, args ...interface{}) (*os.Process, error) {
	cmd := fmt.Sprintf(format, args...)
	logf("Executing command: %s", cmd)
	parts := strings.Split(cmd, " ")
	c := exec.Command(parts[0], parts[1:]...) // #nosec
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("%s command failed: %v", cmd, err)
	}
	return c.Process, nil
}

// ExecuteShellCmd executes a shell command
func (p *PromProxy) execShellCmd(ctx context.Context, logf logging.FormatLogger, format string, args ...interface{}) (string, error) {
	cmd := fmt.Sprintf(format, args...)
	logf("Executing command: %s", cmd)
	c := exec.CommandContext(ctx, "sh", "-c", cmd) // #nosec
	bytes, err := c.CombinedOutput()
	s := string(bytes)
	if err != nil {
		return s, fmt.Errorf("command %q failed with error: %v", cmd, err)
	}

	if output := strings.TrimSuffix(s, "\n"); len(output) > 0 {
		logf("Command output: \n%s", output)
	}

	return s, nil
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
func AllowPrometheusSync(logf logging.FormatLogger) {
	logf("Sleeping to allow prometheus to record metrics...")
	time.Sleep(30 * time.Second)
}

// RunQuery runs a prometheus query and returns the metric value
func RunQuery(ctx context.Context, logf logging.FormatLogger, promAPI v1.API, query string) (float64, error) {
	logf("Running prometheus query: %s", query)

	value, err := promAPI.Query(ctx, query, time.Now())
	if err != nil {
		return 0, nil
	}

	return VectorValue(value)
}

// VectorValue gets the vector value from the value type
func VectorValue(val model.Value) (float64, error) {
	if val.Type() != model.ValVector {
		return 0, fmt.Errorf("Value type is %s. Expected: Valvector", val.String())
	}
	value := val.(model.Vector)
	if len(value) == 0 {
		return 0, errors.New("Query returned no results")
	}

	return float64(value[0].Value), nil
}
