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

// meshdiagnostics preserves failure-time mesh state in opt-in e2e jobs.
// It does not change tests, workloads, routing or resource settings.
package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type event struct {
	Cause     string    `json:"cause"`
	Namespace string    `json:"namespace"`
	Pod       string    `json:"pod"`
	Revision  string    `json:"revision,omitempty"`
	Bucket    string    `json:"bucket,omitempty"`
	At        time.Time `json:"at"`
	// Only diagnostic fields, never request headers, bodies or credentials.
	Record map[string]any `json:"record"`
}

type streak struct {
	first, last time.Time
	count       int
}
type detector struct{ failures map[string]streak }

func (d *detector) record(namespace, pod string, row map[string]any, now time.Time) *event {
	stamp, _ := row["timestamp"].(string)
	if stamp == "" {
		stamp, _ = row["start_time"].(string)
	}
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil || now.Sub(at) > time.Minute || at.Sub(now) > 5*time.Second {
		return nil
	}
	message, _ := row["message"].(string)
	revision, _ := row["knative.dev/key"].(string)
	bucket, _ := row["bucket"].(string)
	identity := namespace + "/" + pod + "/" + bucket + "/" + revision
	failure, _ := row["upstream_transport_failure_reason"].(string)
	var cause string
	switch {
	case strings.Contains(failure, "CERTIFICATE_VERIFY_FAILED") || strings.Contains(failure, "SAN matcher"):
		cause = "tls-identity"
	case message == "Stat forwarding connection established" || strings.HasPrefix(message, "Connected to ws://"):
		delete(d.failures, identity)
		return nil
	case message == "Error while processing stat":
		cause = "forwarding"
	default:
		return nil
	}
	if cause != "tls-identity" {
		if d.failures == nil {
			d.failures = map[string]streak{}
		}
		s := d.failures[identity]
		if s.count == 0 || now.Sub(s.last) > 5*time.Second {
			s = streak{first: now}
		}
		s.last = now
		s.count++
		d.failures[identity] = s
		if s.count < 3 || now.Sub(s.first) < 10*time.Second {
			return nil
		}
	}
	safe := map[string]any{}
	for _, key := range []string{"timestamp", "start_time", "message", "error", "bucket", "retry", "stat_age", "response_code", "response_flags", "upstream_cluster", "upstream_host", "upstream_transport_failure_reason"} {
		if value, ok := row[key]; ok {
			safe[key] = value
		}
	}
	return &event{Cause: cause, Namespace: namespace, Pod: pod, Revision: revision, Bucket: bucket, At: now, Record: safe}
}

// Separate budgets prevent a forwarding outage from consuming SAN evidence.
type budget struct {
	counts     map[string]int
	active     map[string]bool
	identities map[string]time.Time
	lastNormal time.Time
}

func (b *budget) reserve(e event) bool {
	if b.counts == nil {
		b.counts = map[string]int{}
		b.active = map[string]bool{}
		b.identities = map[string]time.Time{}
	}
	category, maximum := "normal", 5
	if e.Cause == "tls-identity" {
		category, maximum = "tls", 3
	}
	key := e.Namespace + "/" + e.Pod + "/" + e.Cause + "/" + e.Bucket + "/" + e.Revision
	if b.active[category] || b.counts[category] >= maximum || e.At.Sub(b.identities[key]) < 5*time.Minute {
		return false
	}
	if category == "normal" && e.At.Sub(b.lastNormal) < 2*time.Minute {
		return false
	}
	b.active[category] = true
	b.counts[category]++
	b.identities[key] = e.At
	if category == "normal" {
		b.lastNormal = e.At
	}
	return true
}

func (b *budget) release(e event) {
	category := "normal"
	if e.Cause == "tls-identity" {
		category = "tls"
	}
	b.active[category] = false
}

// Discard excess output without blocking a subprocess on a full pipe.
type boundedBuffer struct {
	bytes.Buffer
	remaining int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > b.remaining {
		p = p[:b.remaining]
	}
	_, err := b.Buffer.Write(p)
	b.remaining -= len(p)
	return n, err
}

type runner func(context.Context, ...string) ([]byte, error)

func command(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	output := &boundedBuffer{remaining: 8 << 20}
	stderr := &boundedBuffer{remaining: 64 << 10}
	cmd.Stdout = output
	cmd.Stderr = stderr
	err := cmd.Run()
	if err != nil {
		err = fmt.Errorf("%w: %s", err, stderr.String())
	}
	return output.Bytes(), err
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func archive(directory, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	gzipWriter := gzip.NewWriter(file)
	writer := tar.NewWriter(gzipWriter)
	walkErr := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name, err = filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		if err = writer.WriteHeader(header); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		_, err = io.Copy(writer, input)
		return err
	})
	tarErr := writer.Close()
	gzipErr := gzipWriter.Close()
	fileErr := file.Close()
	for _, err := range []error{walkErr, tarErr, gzipErr, fileErr} {
		if err != nil {
			return err
		}
	}
	return nil
}

type pod struct {
	Metadata struct {
		Name   string
		Labels map[string]string
	}
	Spec struct {
		Containers     []struct{ Name string }
		InitContainers []struct{ Name string }
	}
}

func (p pod) proxy() bool {
	for _, c := range append(p.Spec.Containers, p.Spec.InitContainers...) {
		if c.Name == "istio-proxy" {
			return true
		}
	}
	return false
}

type candidate struct {
	namespace string
	pod       pod
	priority  int
}

// Resource allowlists and filtered config dumps deliberately exclude Secrets,
// SDS, tokens, GKE credentials and full cluster-describe responses.
func collect(parent context.Context, run runner, output, systemNamespace, testNamespace string, e event) error {
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	directory, err := os.MkdirTemp(output, "capture-")
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%s", e.At.UTC().Format("20060102T150405.000000000Z"), e.Cause)
	// Archive partial evidence even if an individual command or the deadline fails.
	defer os.RemoveAll(directory)
	defer func() {
		if err := archive(directory, filepath.Join(output, name+".tar.gz")); err != nil {
			log.Printf("archive: %v", err)
		}
	}()
	if err := writeJSON(filepath.Join(directory, "event.json"), e); err != nil {
		return err
	}
	capture := func(file string, args ...string) []byte {
		if ctx.Err() != nil {
			return nil
		}
		data, err := run(ctx, append([]string{"kubectl", "--request-timeout=8s"}, args...)...)
		if writeErr := os.WriteFile(filepath.Join(directory, file), data, 0o600); writeErr != nil {
			log.Printf("capture write: %v", writeErr)
		}
		if err != nil {
			_ = os.WriteFile(filepath.Join(directory, file+".error"), []byte(err.Error()), 0o600)
		}
		return data
	}
	proxyConfig := func(namespace, name string) {
		base := namespace + "-" + name
		// Existing proxy logging is streamed and persisted in GKE. Only collect
		// destination/TLS state that cannot be reconstructed from those logs.
		for _, resource := range []string{"dynamic_endpoint_configs", "dynamic_active_clusters"} {
			capture(base+"-"+resource+".json", "exec", "-n", namespace, name, "-c", "istio-proxy", "--", "pilot-agent", "request", "GET", "config_dump?include_eds&resource="+resource)
		}
	}
	// Envoy state changes within seconds; a TLS identity error can disappear after
	// the next endpoint update. Snapshot the failing proxy before slower
	// Kubernetes queries so its endpoints and allowed SANs match the error.
	if e.Namespace != "" && e.Pod != "" {
		proxyConfig(e.Namespace, e.Pod)
	}
	candidates := []candidate{}
	seen := map[string]bool{}
	for _, ns := range []string{e.Namespace, systemNamespace, "istio-system", testNamespace} {
		if ns == "" || seen[ns] {
			continue
		}
		seen[ns] = true
		capture(ns+"-resources.json", "get", "pods,services,endpointslices,leases,deployments", "-n", ns, "-o", "json")
		capture(ns+"-events.json", "get", "events", "-n", ns, "-o", "json")
		data := capture(ns+"-pods.json", "get", "pods", "-n", ns, "-o", "json")
		var list struct{ Items []pod }
		if json.Unmarshal(data, &list) != nil {
			continue
		}
		for _, p := range list.Items {
			app := p.Metadata.Labels["app"]
			priority := 4
			switch {
			case ns == e.Namespace && p.Metadata.Name == e.Pod:
				// Already captured first.
				continue
			case app == "autoscaler":
				priority = 1
			case app == "istio-ingressgateway":
				priority = 2
			case app == "activator":
				priority = 3
			}
			if priority < 4 && p.proxy() {
				candidates = append(candidates, candidate{ns, p, priority})
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].priority < candidates[j].priority })
	for i, p := range candidates {
		// The target proxy was captured first and counts toward the limit.
		if i >= 7 || ctx.Err() != nil {
			break
		}
		proxyConfig(p.namespace, p.pod.Metadata.Name)
	}
	capture("serving-config.json", "get", "configmaps", "config-autoscaler", "config-leader-election", "config-network", "config-istio", "config-logging", "-n", systemNamespace, "-o", "json")
	return ctx.Err()
}

type options struct{ output, namespace, testNamespace, script string }

func (o options) namespaces() []string {
	// The system namespace is randomized by e2e-common.sh and passed through
	// --namespace. Include all workload namespaces in config/cluster-resources.yaml.
	return []string{o.namespace, "istio-system", o.testNamespace, "serving-tests-alt", "tls", "rq-test"}
}

func sternArgs(o options) []string {
	// Watch only namespaces whose logs the observer can use. Keep native
	// sidecars: istio-proxy can be an init container with restartPolicy Always.
	// Cloud Logging retains complete logs. Only subscribe to containers
	// needed for forwarding/recovery and TLS-identity triggers locally.
	args := []string{".", "--namespace=" + strings.Join(o.namespaces(), ","),
		"--container=^(autoscaler|istio-proxy)$", "--init-containers=true",
		"--color=never", "--timestamps", "--since=1m", "--max-log-requests=2000"}
	// Explicit template retains JSON from application and Envoy logs unchanged.
	return append(args, "--template", `{{.Namespace}} {{.PodName}} {{.ContainerName}} {{.Message}}{{"\n"}}`)
}

func observe(ctx context.Context, run runner, reader io.Reader, o options) error {
	if err := os.MkdirAll(o.output, 0o755); err != nil {
		return err
	}
	started := time.Now().UTC()
	metadata := map[string]any{"started_at": started, "system_namespace": o.namespace, "test_namespace": o.testNamespace, "script": o.script, "log_source": "cloud-logging", "observed_namespaces": o.namespaces()}
	for _, key := range []string{"JOB_NAME", "JOB_TYPE", "BUILD_ID", "PULL_BASE_SHA", "PULL_PULL_SHA", "E2E_PROJECT_ID", "CLOUD_PROVIDER", "ENABLE_GKE_TELEMETRY"} {
		metadata[key] = os.Getenv(key)
	}
	if data, err := run(ctx, "git", "rev-parse", "HEAD"); err == nil {
		metadata["checkout_sha"] = strings.TrimSpace(string(data))
	}
	if data, err := run(ctx, "kubectl", "config", "current-context"); err == nil {
		current := strings.TrimSpace(string(data))
		metadata["context"] = current
		fields := strings.SplitN(current, "_", 4)
		if len(fields) == 4 && fields[0] == "gke" {
			metadata["project"] = fields[1]
			metadata["location"] = fields[2]
			metadata["cluster"] = fields[3]
		}
	}
	if err := writeJSON(filepath.Join(o.output, "run.json"), metadata); err != nil {
		return err
	}
	defer func() {
		metadata["ended_at"] = time.Now().UTC()
		_ = writeJSON(filepath.Join(o.output, "run.json"), metadata)
	}()
	d := detector{}
	b := budget{}
	var mu sync.Mutex
	var workers sync.WaitGroup
	defer workers.Wait()
	namespaces := o.namespaces()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.SplitN(line, " ", 4)
		if len(fields) != 4 {
			continue
		}
		ns, name := fields[0], fields[1]
		if !slices.Contains(namespaces, ns) {
			continue
		}
		pos := strings.IndexByte(fields[3], '{')
		if pos < 0 {
			continue
		}
		var row map[string]any
		if json.Unmarshal([]byte(fields[3][pos:]), &row) != nil {
			continue
		}
		e := d.record(ns, name, row, time.Now())
		if e == nil {
			continue
		}
		mu.Lock()
		allowed := b.reserve(*e)
		mu.Unlock()
		if !allowed {
			continue
		}
		workers.Add(1)
		go func(e event) {
			defer workers.Done()
			defer func() { mu.Lock(); b.release(e); mu.Unlock() }()
			if err := collect(context.Background(), run, o.output, o.namespace, o.testNamespace, e); err != nil {
				log.Printf("partial capture: %v", err)
			}
		}(*e)
	}
	return scanner.Err()
}

func main() {
	var o options
	flag.StringVar(&o.output, "output", "", "artifact directory")
	flag.StringVar(&o.namespace, "namespace", "", "Serving system namespace")
	flag.StringVar(&o.testNamespace, "test-namespace", "serving-tests", "workload namespace")
	flag.StringVar(&o.script, "script", "", "e2e script")
	flag.Parse()
	if o.output == "" || o.namespace == "" {
		log.Fatal("--output and --namespace are required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	cmd := exec.CommandContext(ctx, "stern", sternArgs(o)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	if err := observe(ctx, command, stdout, o); err != nil {
		log.Printf("log observer: %v", err)
	}
	cancel()
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		log.Printf("stern: %v", err)
	}
}
