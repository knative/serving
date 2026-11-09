# EndpointSlice/Pod ordering reproduction

The failed CI snapshots showed bucket 01/09 EDS endpoints with a healthy owner
IP but no `tlsMode` or workload metadata. The corresponding owner accepted TLS
only, so automatic transport selection sent plaintext and forwarding got 503s.
Other buckets for the same IP had correct metadata.

The candidate change adds an explicit `v1/Pod` reference (name and namespace)
from the lease holder to the manually created bucket EndpointSlices. It also
repairs existing references when the IP is unchanged. The retry-return and
traffic-cleanup fixes remain disabled to isolate this candidate.

## Deterministic consumer test

`istio-targetref-ordering-repro.patch` adds a test to a separate Istio 1.29.2
checkout using its existing fake Kubernetes controller:

1. Create a selectorless Service and slice before the Pod is in the cache.
2. Without a Pod reference, observe a cached endpoint with unknown TLS identity.
   Pod arrival does not automatically repair it; an explicit endpoint refresh
   does. This distinguishes ordering/cache behavior from a permanently invalid
   endpoint IP or a rule that manual slices can never use mTLS.
3. With a Pod reference, observe the endpoint deferred and a Pod-arrival resync
   registered. Pod arrival creates the correct TLS mode, namespace, and SPIFFE
   service-account identity automatically.

The cases passed ten repetitions with the race detector. This reproduces the
consumer mechanism suggested by CI, not a real network failure or proof that it
explains the later workload-HA SAN mismatch.

```sh
SERVING_REPO="$PWD"
ISTIO_REPRO="$(mktemp -d)"
git clone --depth=1 --branch=1.29.2 https://github.com/istio/istio.git "$ISTIO_REPRO"
git -C "$ISTIO_REPRO" apply "$SERVING_REPO/test/istio-targetref-ordering-repro.patch"
(
  cd "$ISTIO_REPRO"
  GOMAXPROCS=4 go test -race -p=2 ./pilot/pkg/serviceregistry/kube/controller \
    -run '^TestBucketSlicePodOrderingRepro$' -count=10 -timeout=3m -v
)
```

Go resolves the separate checkout's dependencies. Do not run Serving's
`update-deps.sh`; no Serving dependency pins or generated vendor contents are
changed by this reproduction.

## Producer and network validation

Serving tests verify new references, same-IP upgrades from missing/stale
references, and owner handoff. The existing reconciliation regression fails
before the reference change and passes afterward.

On kind sidecar mesh, the new autoscaler binary emitted ten valid owner
references. An injected client sent twenty marked stats through non-owners
(two paths per bucket), with ownership spread across all three autoscalers.
All twenty arrived intact at their correct owners; all thirty proxy/bucket EDS
views selected mTLS. This checks network integration; CI repetition is still
needed before claiming the natural failure is fixed.

The separate SAN-verification failure needs its own evidence. The monitor now
captures certificate verification errors immediately, including gateways and
workload sidecars, with reserved TLS capacity and the source namespace/pod
prioritized. No certificate private keys or SDS dumps are collected.
