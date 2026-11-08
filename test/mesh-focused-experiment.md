# Focused mesh follow-ups

These modes use the same fresh GKE/Istio setup and Pod-reference fix as the
successful full-suite candidate. They skip the broad conformance/feature suites,
so their passes do not count as full-suite repetitions. Existing HTTP and package
timeouts are unchanged. The retry-return and traffic-cleanup fixes stay disabled.

## `owner-handoff` (#16752)

`TestMeshOwnerHandoffUnderLoad` creates one service and keeps the same traffic
generator running throughout three owner-replacement rounds. It waits for at
least seven ready workload replicas before any deletion. The activator stays in
the request path (`target-burst-capacity: -1`) so live stats exercise forwarding
throughout the test, not only during initial activation.

Before and after replacements it verifies:

- All ten leases refer to live, non-deleting Pods.
- Each slice address and Pod name/namespace matches its lease holder.
- All three ready autoscaler proxies see the correct destination IP, `tlsMode:
  istio`, and autoscaler workload metadata for all ten buckets.
- The current owner accepts live stats for the load revision.

Old/deleting holders cannot satisfy the handoff gate. The load helper checks
sustained scale and its existing traffic SLO, and stops only after all rounds or
on test failure. There is no gap between separate load subtests. The live traffic
exercises one revision bucket; all ten buckets are checked for reference/TLS
correctness, not claimed to have separate marked stat streams in this test.

Successful per-round holder maps and proxy EDS dumps are saved under
`mesh-focused/`. EDS contains no SDS/private-key material. Existing error-triggered
snapshots remain enabled.

Validation: guard tests reject missing/stale references (including same-IP
replacement), deleting holders, wrong addresses, missing buckets, absent TLS mode
and blank workload metadata. Ten race-tested repetitions passed. On kind the
integration test passed with continuous load and three replacement rounds; all
four rounds had correct bucket references/TLS views and live owner receipts.

## `workload-san` (#16753, formerly the no-reference control)

Runs only `TestWorkloadHA/activator-not-in-path`, three times serially, using its
existing two-replica workload, four Pod-replacement rounds and 100% request SLO.
The activator-in-path subtest is excluded by the Go test selector. Its existing
30-minute package timeout is retained, not increased.

Pod references are restored on this branch. The prior no-reference control stays
recorded at `ab66b7493` / build `2119497124218408960`; this is a different experiment.
Immediate SAN/certificate snapshots retain their independent three-capture budget.
A fresh focused pass may miss an age/order-dependent certificate failure, so it
cannot establish that the earlier SAN error is fixed.

Override `MESH_HA_LOAD_MODE=full` to restore the original full sequence. Older
`overlap`, `load-only` and `ha-only` modes remain available. The focused selector
and failure-propagation shell test is `test/mesh-focused-experiment_test.sh`.
