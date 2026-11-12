# Opt-in mesh failure diagnostics

Infra enables `MESH_DIAGNOSTICS=true` only on
`istio-latest-mesh_serving_main_periodic` (the `serving/istio-latest-mesh` TestGrid
tab). It also sets `ENABLE_GKE_TELEMETRY=true` so GKE workload/system logs persist
in Cloud Logging after cluster deletion. Available project audit logs can be
correlated there; this change does not alter Cloud Audit Logs policy or export
audit records into public artifacts.

## Behavior

Without the flag, or without `--mesh`, the test setup/logger is unchanged. With
the flag, `test/mesh-diagnostics.sh` builds this Go command and starts it before
Serving installation. Cloud Logging must already be enabled when debugging;
the collector assumes it provides complete, durable application and Envoy logs.
There is no alternate local log-retention mode.

The existing pinned Stern version streams only `autoscaler` and `istio-proxy`
containers, including native sidecars, in the randomized Serving system namespace
(passed by `e2e-common.sh`), `istio-system`, the primary test namespace, and the
other namespaces declared in `test/config/cluster-resources.yaml`:
`serving-tests-alt`, `tls`, and `rq-test`. These live streams provide low-latency forwarding/recovery and TLS
failure triggers, not log storage: Cloud Logging ingestion can lag a transient
SAN mismatch, and historical logs cannot reconstruct failure-time CDS/EDS state.
The collector replaces the later ordinary logger to avoid duplicate broad log
streams. A failed collector startup falls back to ordinary logging.

The collector does not change tests, routing, policies, resource limits or test
timeouts. It runs for both e2e scripts in the periodic job, using separate artifact
directories for their separate clusters.

## Triggers and limits

- Certificate verification / SAN-matcher failures trigger immediately.
- Stat-processing errors must recur at least three times over ten seconds with
  gaps no greater than five seconds. Expected direct-IP WebSocket failures alone
  do not trigger snapshots. Successful connection logs reset their streak.
- Ignore records more than a minute old and malformed/non-JSON records.
- Five ordinary captures and three independently reserved TLS captures per script.
  One active collector per category; ordinary captures have a two-minute cooldown
  and each identity has a five-minute cooldown.
- Each capture has a 90-second deadline, with ten-second subprocess deadlines and
  eight-MiB command-output caps. Partial results are archived on errors/deadline.
- Live container logs are not copied into artifacts. Complete logs remain in
  Cloud Logging; snapshots retain only the allowlisted trigger record.

Snapshots prioritize the affected Pod/namespace, then autoscalers, gateways and
activators (up to eight proxies). They include Pod/Service/EndpointSlice/Lease/
Deployment state, events, selected Serving configuration and filtered Envoy EDS
and CDS dumps. Existing proxy log levels are reused; snapshots do not fetch
duplicate container logs or listener/route dumps. Pod specs identify service
accounts and native sidecars.
There are no Secret queries, SDS dumps, certificate files, credential exports or
full GKE cluster-describe responses.

## Artifacts and persisted logs

`mesh-diagnostics-<script>/` contains `run.json`, `observer.log` (collector
status/errors, not container logs) and trigger `.tar.gz` files. Each archive
contains an allowlisted `event.json`, resource/proxy captures and per-command
errors. `run.json` records `log_source: cloud-logging`, observed namespaces,
project, cluster, location, start/end timestamps and Prow SHA/build identifiers so
Cloud Logging queries can
be scoped to the matching cluster and run.

## Validation

```sh
go test -race ./test/meshdiagnostics -count=20
# Optional isolated-kind-cluster validation, not a real certificate failure:
KUBECONFIG=/tmp/mesh-kind/kubeconfig MESH_DIAGNOSTICS_LIVE=true \
  go test -race ./test/meshdiagnostics -run '^TestLiveCapture$' -v
```

Tests cover detection/recovery, timestamp filtering, independent budgets,
credential-field exclusion, native-sidecar/target priority, partial archives,
bounded command output, targeted streams, snapshot capture without local log
copies, run metadata and default-off/non-mesh hooks. The optional live test uses
namespaces `knative-serving`, `istio-system` and `serving-tests`; never run it
against a shared or production cluster.
