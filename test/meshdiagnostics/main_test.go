/*
Copyright 2026 The Knative Authors

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

package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDetector(t *testing.T) {
	now := time.Now().UTC()
	row := func(message string, at time.Time) map[string]any {
		return map[string]any{"timestamp": at.Format(time.RFC3339Nano), "message": message, "knative.dev/key": "tests/revision"}
	}
	t.Run("persistent-forwarding", func(t *testing.T) {
		d := detector{}
		for i := range 3 {
			at := now.Add(time.Duration(i) * 5 * time.Second)
			e := d.record("system", "autoscaler", row("Error while processing stat", at), at)
			if (e != nil) != (i == 2) {
				t.Fatalf("trigger %d: %+v", i, e)
			}
		}
	})
	for _, reset := range []string{"recovery", "gap"} {
		t.Run(reset, func(t *testing.T) {
			d := detector{}
			d.record("system", "autoscaler", row("Error while processing stat", now), now)
			later := now.Add(5 * time.Second)
			if reset == "recovery" {
				d.record("system", "autoscaler", row("Connected to ws://bucket", later), later)
			} else {
				later = now.Add(20 * time.Second)
			}
			if e := d.record("system", "autoscaler", row("Error while processing stat", later), later); e != nil {
				t.Fatal(e)
			}
		})
	}
	t.Run("immediate-tls-and-safe-fields", func(t *testing.T) {
		d := detector{}
		r := map[string]any{"start_time": now.Format(time.RFC3339Nano), "upstream_transport_failure_reason": "TLS_error:CERTIFICATE_VERIFY_FAILED:SAN matcher", "authorization": "do-not-save", "body": "do-not-save"}
		e := d.record("istio-system", "gateway", r, now)
		if e == nil || e.Cause != "tls-identity" || e.Namespace != "istio-system" {
			t.Fatal(e)
		}
		if _, ok := e.Record["authorization"]; ok {
			t.Fatal("saved authorization")
		}
		if _, ok := e.Record["body"]; ok {
			t.Fatal("saved body")
		}
	})
	t.Run("ignore-old-and-expected-handshakes", func(t *testing.T) {
		d := detector{}
		for _, at := range []time.Time{now.Add(-2 * time.Minute), now.Add(time.Minute)} {
			if e := d.record("system", "pod", row("Error while processing stat", at), now); e != nil {
				t.Fatal(e)
			}
		}
		for i := range 20 {
			at := now.Add(time.Duration(i) * time.Second)
			if e := d.record("system", "pod", row("Websocket connection could not be established", at), at); e != nil {
				t.Fatal(e)
			}
		}
	})
}

func TestCaptureBudgets(t *testing.T) {
	now := time.Now()
	b := budget{}
	for i := range 5 {
		e := event{Cause: "forwarding", Namespace: "system", Pod: strconv.Itoa(i), At: now.Add(time.Duration(i) * 3 * time.Minute)}
		if !b.reserve(e) {
			t.Fatal("ordinary capture rejected")
		}
		if b.reserve(event{Cause: "forwarding", Pod: "other", At: e.At}) {
			t.Fatal("overlapping ordinary collector")
		}
		b.release(e)
	}
	if b.reserve(event{Cause: "forwarding", Pod: "extra", At: now.Add(time.Hour)}) {
		t.Fatal("ordinary budget exceeded")
	}
	for i := range 3 {
		e := event{Cause: "tls-identity", Namespace: "istio-system", Pod: strconv.Itoa(i), At: now}
		if !b.reserve(e) {
			t.Fatal("ordinary captures consumed TLS budget")
		}
		b.release(e)
		if b.reserve(e) {
			t.Fatal("identity cooldown bypassed")
		}
	}
	if b.reserve(event{Cause: "tls-identity", Pod: "extra", At: now.Add(time.Hour)}) {
		t.Fatal("TLS budget exceeded")
	}
	b = budget{}
	ordinary := event{Cause: "forwarding", Pod: "a", At: now}
	if !b.reserve(ordinary) || !b.reserve(event{Cause: "tls-identity", Pod: "b", At: now}) {
		t.Fatal("TLS waited for ordinary capture/cooldown")
	}
}

func archiveFiles(t *testing.T, path string) map[string][]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	zipped, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer zipped.Close()
	reader := tar.NewReader(zipped)
	files := map[string][]byte{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		files[header.Name] = data
	}
	return files
}

func TestCollectPrioritizesTargetAndExcludesSecrets(t *testing.T) {
	output := t.TempDir()
	var commands []string
	run := func(_ context.Context, args ...string) ([]byte, error) {
		command := strings.Join(args, " ")
		commands = append(commands, command)
		for _, forbidden := range []string{" get secrets", "dynamic_active_secrets", "/certs", "token", "clusters describe", "dynamic_listeners", "dynamic_route_configs", " logs "} {
			if strings.Contains(command, forbidden) {
				t.Fatalf("unsafe command: %s", command)
			}
		}
		if strings.Contains(command, " get pods -n ") {
			name, app := "autoscaler-owner", "autoscaler"
			if strings.Contains(command, "-n tests ") {
				name, app = "workload", ""
			}
			return []byte(fmt.Sprintf(`{"items":[{"metadata":{"name":%q,"labels":{"app":%q}},"spec":{"initContainers":[{"name":"istio-proxy"}]}}]}`, name, app)), nil
		}
		return []byte(`{}`), nil
	}
	e := event{Cause: "tls-identity", Namespace: "tests", Pod: "workload", At: time.Now()}
	if err := collect(context.Background(), run, output, "system", "tests", e); err != nil {
		t.Fatal(err)
	}
	// The failing proxy's EDS and CDS must be captured before any Kubernetes
	// query, and exactly once.
	for i, resource := range []string{"dynamic_endpoint_configs", "dynamic_active_clusters"} {
		if len(commands) <= i || !strings.Contains(commands[i], "exec -n tests workload -c istio-proxy") || !strings.Contains(commands[i], resource) {
			t.Fatalf("command %d = %q, want target proxy %s first", i, commands[i], resource)
		}
	}
	targetCaptures := 0
	for _, command := range commands {
		if strings.Contains(command, "exec -n tests workload ") {
			targetCaptures++
		}
	}
	if targetCaptures != 2 {
		t.Fatalf("target proxy captured %d times, want 2 (EDS and CDS once)", targetCaptures)
	}
	if !strings.Contains(strings.Join(commands, "\n"), "exec -n system autoscaler-owner -c istio-proxy") {
		t.Fatal("other priority proxies were not captured after the target")
	}
	paths, _ := filepath.Glob(filepath.Join(output, "*.tar.gz"))
	if len(paths) != 1 {
		t.Fatal(paths)
	}
	files := archiveFiles(t, paths[0])
	if _, ok := files["tests-workload-dynamic_endpoint_configs.json"]; !ok {
		t.Fatal("native sidecar EDS missing")
	}
	if _, ok := files["event.json"]; !ok {
		t.Fatal("missing trigger record")
	}
}

func TestPartialCaptureIsArchived(t *testing.T) {
	output := t.TempDir()
	run := func(context.Context, ...string) ([]byte, error) { return nil, context.DeadlineExceeded }
	e := event{Cause: "forwarding", Namespace: "system", Pod: "autoscaler", At: time.Now()}
	if err := collect(context.Background(), run, output, "system", "tests", e); err != nil {
		t.Fatal(err)
	}
	paths, _ := filepath.Glob(filepath.Join(output, "*.tar.gz"))
	if len(paths) != 1 {
		t.Fatal(paths)
	}
	files := archiveFiles(t, paths[0])
	if len(files) < 2 {
		t.Fatal("partial evidence lost")
	}
	if _, ok := files["system-resources.json.error"]; !ok {
		t.Fatal("command error not recorded")
	}
}

func TestBoundedCommands(t *testing.T) {
	b := &boundedBuffer{remaining: 4}
	n, err := b.Write([]byte("12345678"))
	if n != 8 || err != nil || b.String() != "1234" {
		t.Fatal(n, err, b.String())
	}
}

func TestSternArgs(t *testing.T) {
	got := sternArgs(options{namespace: "5c15d03a-a25b-4388-afd0-9dad3fac0e17", testNamespace: "tests"})
	want := []string{".", "--namespace=5c15d03a-a25b-4388-afd0-9dad3fac0e17,istio-system,tests,serving-tests-alt,tls,rq-test", "--container=^(autoscaler|istio-proxy)$",
		"--init-containers=true", "--color=never", "--timestamps", "--since=1m", "--max-log-requests=2000",
		"--template", `{{.Namespace}} {{.PodName}} {{.ContainerName}} {{.Message}}{{"\n"}}`}
	if !slices.Equal(got, want) {
		t.Fatalf("stern args = %q, want %q", got, want)
	}
}

func TestObserveCapturesWithoutRetainingLogs(t *testing.T) {
	// Use explicit expectations rather than the implementation's namespace list
	// so omissions (including the randomized system namespace) fail this test.
	for _, namespace := range []string{"5c15d03a-a25b-4388-afd0-9dad3fac0e17", "istio-system", "serving-tests", "serving-tests-alt", "tls", "rq-test"} {
		t.Run(namespace, func(t *testing.T) {
			output := t.TempDir()
			run := func(context.Context, ...string) ([]byte, error) { return []byte(`{}`), nil }
			row := fmt.Sprintf(`{"start_time":%q,"upstream_transport_failure_reason":"CERTIFICATE_VERIFY_FAILED:SAN matcher","authorization":"do-not-save"}`, time.Now().UTC().Format(time.RFC3339Nano))
			input := namespace + " workload istio-proxy " + row + "\n"
			if err := observe(t.Context(), run, strings.NewReader(input), options{output: output, namespace: "5c15d03a-a25b-4388-afd0-9dad3fac0e17", testNamespace: "serving-tests"}); err != nil {
				t.Fatal(err)
			}
			streams, err := filepath.Glob(filepath.Join(output, "stream-*.log"))
			if err != nil || len(streams) != 0 {
				t.Fatalf("duplicate local logs: %v, %v", streams, err)
			}
			data, err := os.ReadFile(filepath.Join(output, "run.json"))
			if err != nil {
				t.Fatal(err)
			}
			var metadata map[string]any
			if err := json.Unmarshal(data, &metadata); err != nil {
				t.Fatal(err)
			}
			if metadata["log_source"] != "cloud-logging" || metadata["ended_at"] == nil {
				t.Fatalf("missing Cloud Logging source/end time: %s", data)
			}
			paths, err := filepath.Glob(filepath.Join(output, "*.tar.gz"))
			if err != nil || len(paths) != 1 {
				t.Fatalf("missing TLS snapshot: %v, %v", paths, err)
			}
			files := archiveFiles(t, paths[0])
			if _, ok := files[namespace+"-workload-dynamic_endpoint_configs.json"]; !ok {
				t.Fatal("target proxy EDS missing")
			}
			if _, ok := files[namespace+"-resources.json"]; !ok {
				t.Fatal("triggering namespace resources missing")
			}
			if data := files["event.json"]; !json.Valid(data) || strings.Contains(string(data), "do-not-save") {
				t.Fatalf("unsafe or missing trigger: %s", data)
			}
		})
	}
}

func TestObserveRetainsMetadataAndFiltersNamespaces(t *testing.T) {
	output := t.TempDir()
	t.Setenv("E2E_PROJECT_ID", "boskos-project")
	t.Setenv("JOB_NAME", "istio-latest-mesh_serving_main_periodic")
	run := func(context.Context, ...string) ([]byte, error) {
		return []byte("gke_boskos-project_us-east1_cluster"), nil
	}
	input := strings.NewReader(fmt.Sprintf("unrelated pod istio-proxy {\"start_time\":%q,\"upstream_transport_failure_reason\":\"CERTIFICATE_VERIFY_FAILED\"}\nsystem autoscaler app plain log\ntests workload istio-proxy plain log\n", time.Now().UTC().Format(time.RFC3339Nano)))
	if err := observe(context.Background(), run, input, options{output: output, namespace: "system", testNamespace: "tests", script: "e2e-tests.sh"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(output, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"project": "boskos-project", "location": "us-east1", "cluster": "cluster", "JOB_NAME": "istio-latest-mesh_serving_main_periodic"} {
		if metadata[key] != want {
			t.Fatal(key, metadata[key])
		}
	}
	if metadata["ended_at"] == nil {
		t.Fatal("missing end time")
	}
	for _, pattern := range []string{"stream-*.log", "*.tar.gz"} {
		paths, err := filepath.Glob(filepath.Join(output, pattern))
		if err != nil || len(paths) != 0 {
			t.Fatalf("unexpected artifacts for %s: %v, %v", pattern, paths, err)
		}
	}
}

// Set MESH_DIAGNOSTICS_LIVE=true and KUBECONFIG for an isolated test cluster.
func TestLiveCapture(t *testing.T) {
	if os.Getenv("MESH_DIAGNOSTICS_LIVE") != "true" {
		t.Skip("opt-in cluster validation")
	}
	data, err := command(t.Context(), "kubectl", "get", "pods", "-n", "istio-system", "-l", "app=istio-ingressgateway", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var pods struct{ Items []pod }
	if err := json.Unmarshal(data, &pods); err != nil || len(pods.Items) == 0 {
		t.Fatal("no gateway", err)
	}
	target := pods.Items[0].Metadata.Name
	output := t.TempDir()
	e := event{Cause: "tls-identity", Namespace: "istio-system", Pod: target, At: time.Now(), Record: map[string]any{"synthetic_validation": true}}
	if err := collect(t.Context(), command, output, "knative-serving", "serving-tests", e); err != nil {
		t.Fatal(err)
	}
	paths, _ := filepath.Glob(filepath.Join(output, "*.tar.gz"))
	if len(paths) != 1 {
		t.Fatal(paths)
	}
	files := archiveFiles(t, paths[0])
	key := "istio-system-" + target + "-dynamic_endpoint_configs.json"
	if !json.Valid(files[key]) {
		t.Fatal("target gateway EDS was not captured")
	}
	t.Logf("Captured %d files, including gateway EDS, without any real TLS failure", len(files))
}

func TestShellHookDisabledByDefault(t *testing.T) {
	for _, env := range [][]string{{"MESH=1"}, {"MESH=0", "MESH_DIAGNOSTICS=true"}} {
		command := exec.Command("bash", "-c", `source ../mesh-diagnostics.sh; function go(){ echo 'unexpected go invocation' >&2; return 1; }; start_mesh_diagnostics; [[ -z "${MESH_DIAGNOSTICS_PID:-}" ]]`)
		command.Env = append([]string{"PATH=" + os.Getenv("PATH")}, env...)
		if data, err := command.CombinedOutput(); err != nil || len(data) > 0 {
			t.Fatal(string(data), err)
		}
	}
}
