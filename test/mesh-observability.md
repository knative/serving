# Temporary mesh CI observability

This draft-PR experiment is observation only. The stat retry-return and traffic
cleanup fixes remain disabled. It does not inject faults or change routing,
mTLS policy, test timeouts, connection/probe timeouts, node counts, machine types,
or Serving/Istio resource limits. Enabling GKE agents and additional counters has
observation overhead; compare the recorded resource usage when interpreting
run-to-run timing.
Do not run `update-deps.sh` while using these one-off vendored script changes.

## Lifecycle

`test/e2e-tests.sh` defaults `MESH_OBSERVABILITY=true`. The vendored GKE creation
hook checks that mesh is enabled, exports `ENABLE_GKE_TELEMETRY=true` before
`kubetest2 --up`, and requests `--logging=SYSTEM,WORKLOAD --monitoring=SYSTEM`.
Non-mesh cluster creation keeps its existing settings.

The inner setup starts observation before `knative_setup`:

- Reuse the existing unfiltered, all-namespace Stern stream, explicitly including
  native sidecars/init containers. Starting early captures installation logs.
  The usual image-upload phase stays after installation and does not create a
  second logger.
- Read back the actual GKE logging/monitoring components and record the cluster
  identity, Kubernetes versions, node configuration, checkout/PR/base SHAs and
  creation-time observation window. Cluster `masterAuth` is never saved.
- Capture compressed Kubernetes state, node/container usage, and filtered CPU/CFS
  throttling counters approximately every 30 seconds. Record capture exit codes, including expected missing
  namespaces/CRDs during initial installation.
- Read live autoscaler/activator/gateway Envoy counters, autoscaler endpoint
  health, and Istiod metrics. An observation-only ytt overlay enables relevant
  proxy connection/TLS/queue/endpoint counters; no proxy behavior is changed.

On normal success/failure exit, stop the observer and export Cloud Logging and
Cloud Monitoring data before logger/resource cleanup and before kubetest2
releases the Boskos project. Normal cluster deletion remains enabled.
Individual API calls and the overall Cloud export have deadlines. SIGKILL cannot
run cleanup hooks; continuous snapshots/logs still remain in the artifact area.

## Artifacts

Under `${ARTIFACTS}`:

- `log-e2e-tests.tar.gz`: unfiltered component and native-sidecar logs.
- `mesh-observability-e2e-tests/run.json`: run/cluster identities and time window.
- `mesh-observability-e2e-tests/gke-config.json`: sanitized effective configuration.
- `mesh-observability-e2e-tests/samples/<UTC time>/`: state, usage, proxy and
  Istiod measurements, plus `capture-status.json`.
- `mesh-observability-e2e-tests/cloud/`: paginated, compressed Cloud log entries
  and CPU/memory/restart time series, scoped to this cluster/location/time window.
- `mesh-observability-e2e-tests/export-status.json`: exported counts, errors,
  deadlines and explicit incomplete-query flags. A noisy namespace is capped at
  100,000 Cloud entries; Stern logs are retained independently without that cap.
- `observer.log`, `export.log`, and error JSON files: collector health.

Cloud Logging ingestion can lag. Query completion does not imply every final
entry has already been ingested. Run metadata preserves the filter coordinates
for later queries. GCP logging/monitoring read permissions are required; denied
exports are reported rather than changing the test outcome.

Kubernetes Secrets, audit logs, Envoy SDS dumps, GKE master credentials, and
access tokens are not collected. Access tokens are used only in memory for the
Cloud API calls. Kind validation skips Cloud exports entirely.

## Local validation

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s test \
  -p mesh_observability_test.py -v
bash -n test/mesh-observability.sh test/e2e-tests.sh test/e2e-common.sh \
  vendor/knative.dev/hack/e2e-tests.sh vendor/knative.dev/hack/infra-library.sh
```

Use `MESH_OBSERVABILITY=false` to opt out or `MESH_OBSERVABILITY_INTERVAL=30` to
set the sampling interval. This is step 1 only: automatic failure triggers,
focused reproductions, and controlled stress experiments are not added yet.
