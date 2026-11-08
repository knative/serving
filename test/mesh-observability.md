# Temporary mesh CI observability

This draft-PR investigation now tests a single candidate fix: explicit Pod
references on bucket EndpointSlices. The stat retry-return and traffic cleanup
fixes remain disabled. See `istio-targetref-repro.md` for the ordering reproduction
and the limits of what this candidate has established. It does not inject faults or change routing,
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

Cloud Logging reads are paced across namespaces, retry HTTP 429 with backoff,
and prioritize a small forwarding/activation query before bulk logs. Entries are
exported newest-first so a bounded export does not retain only startup logs.
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
set the sampling interval. No network/resource faults are injected.

## Error-triggered snapshots and original-order runs

The default is now `MESH_HA_LOAD_MODE=full`: run the original main/conformance,
feature-toggle, HPA and HA sequence. The two actual focused overlap runs passed
at `c6f7769a4` (builds `2119262045986623488` and `2119273311182524416`), so this
experiment returns to the longer original workload instead of adding more load.

An independent monitor reads the existing unfiltered autoscaler/activator logs
while periodic sampling continues. It triggers after at least three repeated
errors spanning ten seconds, resetting after a five-second gap or a recovery:

- Stat-forwarding processing failures or bucket-Service DNS connection failures.
  Expected direct Pod-IP handshake failures are ignored.
- Activator stat-send failures.
- ClusterIP probe failures when Kubernetes reports ready backends.

The monitor also reads gateway and workload-sidecar logs. Certificate/SAN
verification errors trigger immediately, without waiting for repeated failures.
The monitor ignores stale log records. It reserves three TLS-identity captures
in addition to five ordinary captures; earlier forwarding failures cannot consume
that budget. At most one collector of each category can run, so a TLS snapshot
is not delayed behind an ordinary capture. Ordinary captures keep their two-minute
global cooldown; all identities retain a five-minute cooldown. A trigger does not
fail or interrupt any test.

`triggers/<phase>/event.json` records the source, revision/bucket, timing, retry
and queue context. `mesh-diagnostics-trigger-*.tar.gz` contains the existing
secret-safe Kubernetes/proxy capture, prioritizing the affected namespace/pod
(including gateways or workload sidecars) and all autoscalers before other pods. Each collector is bounded to 90 seconds;
partial results are archived on termination rather than discarded.

This observes forwarding/probe failures; it does not add in-flight request
queue timers or change request handling. Existing queue-wait diagnostics remain
available. The goal is to get proxy state before Go test cleanup removes the
failed revision, not to infer a cause from a trigger alone.

## Focused HA/load experiment

The full-suite baseline passed at `c30f08250` / Prow build
`2119221083113525248`, with the fixes disabled. The focused experiment used
`MESH_HA_LOAD_MODE=overlap` for full mesh runs:

- Run only `TestAutoscaleSustaining` twice in a background Go test invocation.
- Wait for at least seven ready workload replicas to establish that load is
  active, then run `TestActivatorHAGraceful` alongside it. Match the stable
  `serving.knative.dev/service` label, not hashed/truncated Deployment names,
  and exclude Deployments already being deleted.
- Wait for both invocations to finish normally; pass only if both pass. Do not
  kill the load test or run global feature-toggle suites concurrently.
- Preserve the original 50-minute load-package and 30-minute HA-package timeouts,
  cluster/resources, telemetry, mesh configuration, and disabled fixes.
- Record mode, readiness and start/finish markers, load output, and both results
  under `mesh-ha-load/`. A missing load prerequisite is identified separately
  by `ha_started=0`; it is not evidence of an HA-test failure.

The first attempt (`83f24ceee`, build `2119253490520821760`) passed both load
repetitions but never started HA because the name-based readiness filter missed
the hashed Deployments. The corrected predicate is tested against an actual CI
snapshot with 11 ready replicas and has a regression test:
`bash test/mesh-ha-load-readiness_test.sh`.

This is a focused, fresh-cluster experiment, not the original periodic workload:
it skips the full-suite warmup and repeats the load. Passing does not rule out
aging/earlier-test effects; a generic SLO failure under load does not establish
the original CI cause. Inspect the trace for the same persistent forwarding or
activation failure. The load has normal gaps between its aggregation variants.

Use `MESH_HA_LOAD_MODE=full` to restore the full original suite/order, `load-only`
for a matched load control, or `ha-only` for a fresh-cluster HA control. These
modes use the same PR and the ordinary `/test istio-latest-mesh` trigger.
