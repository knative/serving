#!/usr/bin/env python3

# Copyright 2026 The Knative Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Temporary passive mesh observer. No faults, Secrets, or dependency updates."""

import argparse
import datetime
import gzip
import json
import os
from pathlib import Path
import signal
import subprocess
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

import mesh_log_triggers

# Cloud Logging entries.list has a small read-request quota. Share the pacing
# budget across all log exports in this process, not separately per namespace.
_next_log_request_at = 0.0


def utc_now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def write_json(path, data):
    path.write_text(json.dumps(data, indent=2) + "\n")


def command(args, timeout=10):
    try:
        result = subprocess.run(args, capture_output=True, timeout=timeout)
        return result.returncode, result.stdout, result.stderr
    except subprocess.TimeoutExpired:
        return 124, b"", b"command timed out\n"
    except OSError as exc:
        return 127, b"", (str(exc) + "\n").encode()


def telemetry_status(config):
    logging = ((config.get("loggingConfig") or {}).get("componentConfig") or {}).get("enableComponents") or []
    monitoring = ((config.get("monitoringConfig") or {}).get("componentConfig") or {}).get("enableComponents") or []
    return {
        "logging_components": logging,
        "monitoring_components": monitoring,
        "system_and_workload_logging_enabled": {"SYSTEM_COMPONENTS", "WORKLOADS"}.issubset(logging),
        "system_monitoring_enabled": "SYSTEM_COMPONENTS" in monitoring,
    }


def discover(args):
    metadata = {
        "started_at": os.environ.get("MESH_OBSERVABILITY_STARTED_AT", utc_now()),
        "provider": args.provider,
        "project": args.project,
        "system_namespace": args.namespace,
        "test_namespace": os.environ.get("TEST_NAMESPACE", "serving-tests"),
        "snapshot_interval_seconds": args.interval,
        "experiment_mode": "full" if os.environ.get("SHORT") == "1" else os.environ.get("MESH_HA_LOAD_MODE", "full"),
        "prow": {k: os.environ.get(k) for k in ("JOB_NAME", "JOB_TYPE", "BUILD_ID", "PULL_PULL_SHA", "PULL_BASE_SHA")},
    }
    repository = str(Path(__file__).resolve().parent.parent)
    rc, stdout, stderr = command(["git", "-C", repository, "rev-parse", "HEAD"])
    metadata["checkout_sha"] = stdout.decode().strip() if rc == 0 else None
    rc, stdout, stderr = command(["go", "version"])
    metadata["go_version"] = stdout.decode().strip() if rc == 0 else None
    image_commit = Path("/commit_hash")
    metadata["runner_image_commit"] = image_commit.read_text().strip() if image_commit.exists() else None
    pin = Path(repository) / "third_party/istio-latest/net-istio.yaml"
    if pin.exists():
        metadata["net_istio_pin"] = pin.read_text().splitlines()[0]
    rc, stdout, stderr = command(["kubectl", "config", "current-context"])
    context = stdout.decode().strip() if rc == 0 else ""
    metadata["context"] = context
    rc, stdout, stderr = command(["kubectl", "--request-timeout=5s", "get", "namespace", "kube-system", "-o", "json"])
    if rc == 0:
        metadata["kube_system_namespace_uid"] = json.loads(stdout)["metadata"]["uid"]
    if args.provider == "gke" and context.startswith("gke_"):
        _, project, location, cluster = context.split("_", 3)
        if project != args.project:
            raise ValueError("GKE context project differs from the Boskos project")
        metadata.update(location=location, cluster=cluster)
        rc, stdout, stderr = command([
            "gcloud", "container", "clusters", "describe", cluster,
            "--project", project, "--location", location, "--format=json",
        ], timeout=20)
        if rc == 0:
            config = json.loads(stdout)
            # Never persist masterAuth or credentials returned by cluster describe.
            safe = {k: config.get(k) for k in (
                "name", "id", "location", "currentMasterVersion", "currentNodeVersion",
                "loggingService", "monitoringService", "loggingConfig", "monitoringConfig",
                "addonsConfig", "releaseChannel", "network", "subnetwork", "ipAllocationPolicy",
            )}
            safe["nodePools"] = [{"name": p.get("name"), "config": {
                k: (p.get("config") or {}).get(k) for k in ("machineType", "imageType", "spot", "preemptible")
            }} for p in (config.get("nodePools") or [])]
            write_json(args.output / "gke-config.json", safe)
            metadata["telemetry"] = telemetry_status(config)
        else:
            (args.output / "gke-config.stderr").write_bytes(stderr)
            metadata["telemetry"] = {"verified": False, "describe_exit_code": rc}
    write_json(args.output / "run.json", metadata)
    print("Observer metadata:", json.dumps(metadata), flush=True)
    return metadata


def sample(args, stop):
    directory = args.output / "samples" / utc_now().replace(":", "")
    directory.mkdir(parents=True, exist_ok=True)
    statuses = {}
    pods = []
    nodes = []
    namespaces = [args.namespace, "istio-system", os.environ.get("TEST_NAMESPACE", "serving-tests")]
    captures = [("nodes.json", ["get", "nodes", "-o", "json"]), ("node-usage.txt", ["top", "nodes"])]
    for namespace in dict.fromkeys(namespaces):
        captures.extend([
            (namespace + "-state.json", ["get", "pods,services,endpointslices,leases,deployments,events", "-n", namespace, "-o", "json"]),
            (namespace + "-usage.txt", ["top", "pods", "-n", namespace, "--containers"]),
        ])
    captures.append(("revisions.json", ["get", "services.serving.knative.dev,revisions.serving.knative.dev,podautoscalers.autoscaling.internal.knative.dev,serverlessservices.networking.internal.knative.dev", "-n", namespaces[-1], "-o", "json"]))
    for filename, flags in captures:
        if stop():
            break
        rc, stdout, stderr = command(["kubectl", "--request-timeout=5s", *flags], timeout=7)
        with gzip.open(directory / (filename + ".gz"), "wb") as output:
            output.write(stdout)
        if stderr:
            (directory / (filename + ".stderr")).write_bytes(stderr)
        statuses[filename] = rc
        if filename == "nodes.json" and rc == 0:
            try:
                nodes = json.loads(stdout).get("items", [])
            except ValueError:
                pass
        if filename.endswith("-state.json") and rc == 0:
            try:
                pods.extend(item for item in json.loads(stdout).get("items", []) if item.get("kind") == "Pod")
            except ValueError:
                pass

    # Read live counters, not access logs alone: long-lived WebSocket access
    # records can be emitted only after their connections close.
    for pod in pods:
        if stop():
            break
        namespace, name = pod["metadata"]["namespace"], pod["metadata"]["name"]
        if pod["metadata"].get("deletionTimestamp") or pod.get("status", {}).get("phase") != "Running":
            continue
        app = pod["metadata"].get("labels", {}).get("app", "")
        if app in ("autoscaler", "activator", "istio-ingressgateway"):
            statuses_for_pod = pod.get("status", {}).get("containerStatuses", []) + pod.get("status", {}).get("initContainerStatuses", [])
            if not any(c["name"] == "istio-proxy" and "running" in c.get("state", {}) for c in statuses_for_pod):
                continue
            captures = [("envoy-stats.txt", "stats?usedonly")]
            if app == "autoscaler":
                captures.append(("envoy-clusters.json", "clusters?format=json"))
            for filename, path in captures:
                if stop():
                    break
                rc, stdout, stderr = command(["kubectl", "--request-timeout=5s", "exec", "-n", namespace, name,
                                             "-c", "istio-proxy", "--", "pilot-agent", "request", "GET", path], timeout=7)
                file = name + "-" + filename
                with gzip.open(directory / (file + ".gz"), "wb") as output:
                    output.write(stdout)
                if stderr:
                    (directory / (file + ".stderr")).write_bytes(stderr)
                statuses[file] = rc
        elif app == "istiod":
            rc, stdout, stderr = command(["kubectl", "--request-timeout=5s", "get", "--raw",
                                         f"/api/v1/namespaces/{namespace}/pods/{name}:15014/proxy/metrics"], timeout=7)
            file = name + "-istiod-metrics.txt"
            with gzip.open(directory / (file + ".gz"), "wb") as output:
                output.write(stdout)
            if stderr:
                (directory / (file + ".stderr")).write_bytes(stderr)
            statuses[file] = rc
    # Keep only CPU/CFS counters from cAdvisor, not its full payload. These
    # expose throttling that aggregate CPU usage can miss.
    for node in nodes:
        if stop():
            break
        name = node["metadata"]["name"]
        rc, stdout, stderr = command(["kubectl", "--request-timeout=5s", "get", "--raw",
                                     f"/api/v1/nodes/{name}/proxy/metrics/cadvisor"], timeout=7)
        counters = b"\n".join(line for line in stdout.splitlines()
                              if line.startswith((b"container_cpu_cfs_", b"container_cpu_usage_seconds_total")))
        file = name + "-cpu-cfs.txt"
        with gzip.open(directory / (file + ".gz"), "wb") as output:
            output.write(counters)
        if stderr:
            (directory / (file + ".stderr")).write_bytes(stderr)
        statuses[file] = rc
    write_json(directory / "capture-status.json", statuses)


def observe(args):
    stopping = False

    def on_signal(signum, frame):
        nonlocal stopping
        stopping = True

    signal.signal(signal.SIGTERM, on_signal)
    signal.signal(signal.SIGINT, on_signal)
    stop = lambda: stopping or (args.output / "stop").exists()
    watcher = None
    try:
        discover(args)
        if args.logs:
            def watch_logs():
                try:
                    mesh_log_triggers.monitor(args, stop)
                except Exception as exc:
                    write_json(args.output / "trigger-error.json", {"error": str(exc)})
                    print("Mesh trigger monitor failed:", str(exc), flush=True)
            watcher = threading.Thread(target=watch_logs, daemon=True)
            watcher.start()
        while not stop():
            sample(args, stop)
            deadline = time.monotonic() + args.interval
            while not stop() and time.monotonic() < deadline:
                time.sleep(0.25)
    finally:
        stopping = True
        if watcher:
            watcher.join(timeout=35)
        write_json(args.output / "stopped", {"stopped_at": utc_now()})


def cloud_filter(metadata, namespace=None):
    base = (
        f'resource.labels.cluster_name={json.dumps(metadata["cluster"])} AND '
        f'resource.labels.location={json.dumps(metadata["location"])} AND '
        f'timestamp>={json.dumps(metadata["started_at"])} AND '
        f'timestamp<={json.dumps(metadata["ended_at"])} AND '
        'NOT logName:"cloudaudit.googleapis.com"'
    )
    if namespace:
        return base + f' AND resource.type="k8s_container" AND resource.labels.namespace_name={json.dumps(namespace)}'
    return base + ' AND (resource.type="k8s_node" OR resource.type="k8s_control_plane_component" OR resource.type="k8s_cluster")'


def api(url, token, body=None, timeout=10):
    data = json.dumps(body).encode() if body is not None else None
    request = urllib.request.Request(url, data=data, headers={
        "Authorization": "Bearer " + token, "Content-Type": "application/json",
    })
    with urllib.request.urlopen(request, timeout=timeout) as response:
        return json.load(response)


def export_logs(metadata, token, output, name, query, deadline, limit=100000):
    global _next_log_request_at
    count, page_token, complete = 0, "", False
    rate_limited, last_error = 0, None
    with gzip.open(output / (name + ".jsonl.gz"), "wt") as stream:
        while time.monotonic() < deadline and count < limit:
            wait = max(0, _next_log_request_at - time.monotonic())
            if time.monotonic() + wait >= deadline:
                break
            time.sleep(wait)
            _next_log_request_at = time.monotonic() + 1.6
            try:
                response = api("https://logging.googleapis.com/v2/entries:list", token, {
                    "resourceNames": ["projects/" + metadata["project"]], "filter": query,
                    "orderBy": "timestamp desc", "pageSize": min(1000, limit - count), "pageToken": page_token,
                }, timeout=max(0.1, min(10, deadline - time.monotonic())))
            except urllib.error.HTTPError as exc:
                if exc.code != 429:
                    exc.close()
                    raise
                rate_limited += 1
                last_error = str(exc)
                try:
                    delay = float(exc.headers.get("Retry-After", "5"))
                except (AttributeError, ValueError):
                    delay = 5
                _next_log_request_at = time.monotonic() + max(5, delay)
                exc.close()
                continue
            for entry in response.get("entries", []):
                stream.write(json.dumps(entry) + "\n")
                count += 1
            page_token = response.get("nextPageToken", "")
            if not page_token:
                complete = True
                break
    return {"entries": count, "query_completed": complete, "filter": query,
            "rate_limited_requests": rate_limited, "last_error": last_error,
            "note": "Recent entries first; Cloud ingestion may lag. Stern logs are retained separately."}


def export_metrics(metadata, token, output, deadline):
    results = {}
    metrics = [
        "kubernetes.io/node/cpu/allocatable_utilization",
        "kubernetes.io/node/memory/allocatable_utilization",
        "kubernetes.io/container/cpu/core_usage_time",
        "kubernetes.io/container/cpu/request_utilization",
        "kubernetes.io/container/cpu/limit_utilization",
        "kubernetes.io/container/memory/used_bytes",
        "kubernetes.io/container/restart_count",
    ]
    for metric in metrics:
        if time.monotonic() >= deadline:
            results[metric] = {"query_completed": False, "reason": "export deadline"}
            continue
        query = (f'metric.type={json.dumps(metric)} AND '
                 f'resource.labels.cluster_name={json.dumps(metadata["cluster"])} AND '
                 f'resource.labels.location={json.dumps(metadata["location"])}')
        count, page_token, complete = 0, "", False
        try:
            with gzip.open(output / (metric.replace("/", "_") + ".jsonl.gz"), "wt") as stream:
                while time.monotonic() < deadline:
                    params = urllib.parse.urlencode({
                        "filter": query, "interval.startTime": metadata["started_at"],
                        "interval.endTime": metadata["ended_at"], "view": "FULL",
                        "pageSize": 1000, "pageToken": page_token,
                    })
                    response = api("https://monitoring.googleapis.com/v3/projects/" + metadata["project"] + "/timeSeries?" + params,
                                   token, timeout=max(0.1, min(10, deadline - time.monotonic())))
                    for series in response.get("timeSeries", []):
                        stream.write(json.dumps(series) + "\n")
                        count += 1
                    page_token = response.get("nextPageToken", "")
                    if not page_token:
                        complete = True
                        break
            results[metric] = {"series": count, "query_completed": complete}
        except Exception as exc:
            results[metric] = {"series": count, "query_completed": False, "error": str(exc)}
    return results


def export(args):
    metadata = json.loads((args.output / "run.json").read_text())
    metadata["ended_at"] = utc_now()
    write_json(args.output / "run.json", metadata)
    if args.provider != "gke":
        write_json(args.output / "export-status.json", {"status": "not applicable to " + args.provider})
        return
    if not metadata.get("cluster"):
        raise ValueError("GKE cluster identity was not discovered; refusing an unscoped export")
    rc, stdout, stderr = command(["gcloud", "auth", "print-access-token"])
    if rc != 0:
        raise RuntimeError("Unable to obtain GCP token; Cloud export requires logging/monitoring read access")
    token = stdout.decode().strip()  # Kept only in memory; never included in artifacts or logs.
    output = args.output / "cloud"
    output.mkdir(exist_ok=True)
    status = {}
    deadline = time.monotonic() + 180
    critical = "(Error while processing stat|Exceeding max retries|[Ss]tat forwarding connection|Websocket connection|Autoscaler connection (lost|established)|Stats were not sent|Throttler try error|Request acquired capacity)"
    queries = {"forwarding-activation": cloud_filter(metadata, args.namespace) +
               f" AND (jsonPayload.message =~ {json.dumps(critical)} OR textPayload =~ {json.dumps(critical)})",
               "system": cloud_filter(metadata)}
    for namespace in dict.fromkeys([args.namespace, "istio-system", metadata["test_namespace"]]):
        queries["workload-" + namespace] = cloud_filter(metadata, namespace)
    # Reserve time for each export so a noisy workload cannot consume the budget.
    for name, query in queries.items():
        try:
            status[name] = export_logs(metadata, token, output, name, query, min(deadline, time.monotonic() + 25))
        except Exception as exc:
            status[name] = {"query_completed": False, "error": str(exc)}
    try:
        status["metrics"] = export_metrics(metadata, token, output, deadline)
    except Exception as exc:
        status["metrics"] = {"query_completed": False, "error": str(exc)}
    write_json(args.output / "export-status.json", status)
    print("Cloud export status:", json.dumps(status), flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["observe", "export"])
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--namespace", required=True)
    parser.add_argument("--logs", type=Path, help="Existing unfiltered Stern log directory; enables error-triggered snapshots")
    parser.add_argument("--project", default="")
    parser.add_argument("--provider", choices=["gke", "kind"], required=True)
    parser.add_argument("--interval", type=float, default=30)
    args = parser.parse_args()
    if args.interval <= 0:
        parser.error("interval must be positive")
    args.output.mkdir(parents=True, exist_ok=True)
    try:
        if args.action == "observe":
            observe(args)
        else:
            export(args)
    except Exception as exc:
        write_json(args.output / (args.action + "-error.json"), {"error": str(exc)})
        raise


if __name__ == "__main__":
    main()
