# Init-container and native-sidecar tests

## Environment

Use an isolated Kubernetes **1.35.x** cluster, including its worker nodes. Native
sidecars are stable on this baseline; no SidecarContainers gate is required.
Record exact patch versions, node images, Serving commit, image tag, and test
results. These tests do not establish support for other Kubernetes minors.

Install the local Serving build and a networking provider using
[development deployment](../../../DEVELOPMENT.md#deploy-knative-serving).
Historical version examples there do not override this checkout's `go.mod` or
the 1.35.x PoC baseline. Enable `kubernetes.podspec-init-containers` in the
`knative-serving/config-features` ConfigMap before running this package. Changing
the flag or installing components requires explicit cluster-mutation approval.

The harness needs ordinary test resource permissions plus `create` access to
`pods/portforward` and Pod get/list/watch. It connects directly to the Kubernetes
port-forward subresource: unlike the kubectl command, it must work while the Pod
is Pending during initialization. Failure to establish this channel is a failed
test, not a skipped ordering assertion. The helper itself needs no additional
RBAC or production credentials.

## Local checks

```sh
go test ./pkg/apis/serving/... ./pkg/reconciler/service/resources ./pkg/reconciler/configuration/resources ./pkg/reconciler/revision/resources
go test -race ./test/test_images/readiness
go test -tags=e2e -run '^$' ./test/e2e/initcontainers
```

## Run against the approved cluster

Rebuild the **existing** readiness image; do not register a new image. Follow
[test-image instructions](../../README.md#building-the-test-images). Publishing
images and installing the modified Serving build are separate mutations that
need approval. Set all variables below to the approved environment before use.

```sh
./test/upload-test-images.sh "$TEST_IMAGE_TAG"
go test -v -tags=e2e -count=1 ./test/e2e/initcontainers \
  --kubeconfig "$KUBECONFIG" --cluster "$TEST_CLUSTER" \
  --dockerrepo "$KO_DOCKER_REPO" --tag "$TEST_IMAGE_TAG" \
  --ingressendpoint "$INGRESS_ENDPOINT"
```

See [test flags](../../README.md#flags) for resolvable-domain and alternate gateway
settings. Run the new tests repeatedly before claiming stable ordering evidence.

For a local kind cluster, `--ingressendpoint` must be a **host only**, not
`host:port`: the test client obtains its port from the gateway Service's NodePort.
If using port-forwarding, bind the local port to that HTTP NodePort and forward
it to gateway port 80. Set `GATEWAY_OVERRIDE=kourier` and
`GATEWAY_NAMESPACE_OVERRIDE=kourier-system` when using Kourier. Configure a test
domain such as `example.com`; the default cluster-local domain is not served by
the public Kourier gateway.

When importing image archives rather than publishing a registry, use locally
loaded references and restrict tag-resolution skipping to their registry name.
The ordinary `TestInitContainers` regression expects image digests in Revision
status; run it with a digest-qualified volumes image rather than treating skipped
tag resolution as a passing digest test. Do not weaken its assertions.

The success test creates a one-replica Service without waiting for readiness,
observes a failing helper startup endpoint and unstarted application/queue-proxy,
then releases health through loopback port-forwarding. It requires an unchanged
Pod UID, no helper restart during release, Service readiness, and a successful
routed request. The failure test leaves health blocked until kubelet restarts the
helper and verifies regular containers have not started. Both use synthetic data,
bounded waits, and graceful cleanup; cleanup failure is reported, not hidden by
force deletion. Each helper requests 25m CPU/32Mi and is limited to 100m/64Mi.

Startup success is an initial gate, not ongoing health monitoring. Readiness and
liveness probes and lifecycle hooks on init containers remain unsupported.
Knative reuses its existing probe checks; Kubernetes can reject additional invalid
settings later when creating the generated workload. Helper-specific Revision
diagnostics, higher-scale behavior, and helper-aware autoscaling are not claimed.

## Serial admission-only flag-disable check

This is a manual check in the approved isolated cluster, not part of parallel
e2e execution. Approval must cover config changes and deletion of the **test Pod**.
Use a separate, identifiable test namespace. Save the original config-features
data securely and arrange restoration on failure; do not log unrelated config or
credentials. Bound each state wait to two minutes and the procedure to ten minutes.

1. With the flag enabled, create a Service equivalent to the success fixture,
   including min/max scale 1. Record its Revision and Pod UIDs and helper fields.
   Release its blocked endpoint using direct forwarding and verify a request.
2. Disable only the init-container flag. Poll server-side dry-run creation of a
   fresh Service with init containers until the webhook rejects it specifically
   because init-container support is disabled. Then verify real creation of a
   uniquely named equivalent Service is rejected for the same reason. RBAC or
   connectivity errors do not count as success.
3. Verify the existing Revision UID is unchanged, its Pod remains usable, and the
   helper fields remain intact. Flag disablement must not remove admitted helpers.
4. Gracefully delete only the tracked Pod using a UID precondition. Require that
   it disappears and a different Pod UID for the same Revision appears. Verify
   preserved native-sidecar fields and blocked regular containers before releasing
   the replacement helper through a new direct forwarding session. Require a
   successful request without a new Revision.
5. Restore the original flag data, delete the test Service normally, and verify
   its Pods disappear. Report failed restoration or cleanup as failure.

New-Service acceptance after convergence, premature application startup, lost
fields, loss of existing-Pod availability before deliberate deletion, or failed
replacement blocks PoC completion. This check does not establish binary-downgrade
safety. Inspect generated Pod resource requests and the Knative calculation paths;
record accounting discrepancies before broader adoption rather than inferring
autoscaler accuracy from a passing request.
