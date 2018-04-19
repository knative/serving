# 2018 Autoscaling Roadmap

This is what we hope to accomplish in 2018.

## References

[Autoscaling Design Goals](README.md#design-goals):

  1. *Make it fast*
  2. *Make it light*
  3. *Make everything better*

In 2018 we will focus primarily on making autoscaling correct, fast and light.

## Areas of Interest and Requirements

1. **Correctness**.  When scaling from 0-to-1, 1-to-N and back down, error rates must not increase.  We must have visibility of correctness over time at small and large scales.
2. **Performance**.  When scaling from 1-to-N and back down, autoscaling must maintain reasonable latency.  The Elafros implementation of autoscaling must be competitive in its ability to serve variable load.
3. **Scale to zero**.  Idle ([Reserve](README.md#behavior)) Revisions must cost nothing.  Reserve Revisions must serve the first request in 1 second or less.
4. **Development**.  Autoscaler development must follow a clear roadmap.  Getting started as a developer must be easy and the team must scale horizontally.
5. **Integration**.  Autoscaler should be pluggable and support multiple strategies and workloads.

### Correctness

1. **Write autoscaler conformance tests** to cover low-scale regressions, runnable by individual developers before checkin.
2. **Test error rates at high scale** to cover regressions at larger scales (~1000 QPS and ~1000 clients).
3. **Test error rates around idle states** to cover various scale-to-zero edge cases.

### Performance

1. **Establish canonical load test scenarios** to prove autoscaler performance and guide development.  We need to establish the code to run, the request load to generate, and the performance expected.  This will tell us where we need to improve.
2. **Reproducable load tests** which can be run by anyone with minimal setup.  These must be transparent and easy to run.  They must be meaningful tests which prove autoscaler performance.
3. **[Slow Brain](README.md#slow-brain--fast-brain) implementation** to automatically adjust autoscaling parameters to the application's behavior.

### Scale to Zero

1. **Reduce Reserve Revision start time** from 4 seconds to 1 second or less.  Maybe we can keep some resources around like the Replica Set or pre-cache images so that Pods are spun up faster.

### Development

1. **Custom resource definition and controller** to encapsulate the autoscaling implementation.  This will let autoscaling evolve independently from the Revision custom resource and controller.  This makes development more independent and scalable.
2. **Remove metrics reporting from the Queue Proxy** in order to rely on a common, Elafros metrics pipeline.  This could mean polling the Pods to get the same metrics as are reported to Prometheus.  Or going to Prometheus to get the metrics it has aggregated.  It means removing the metrics push from the [Queue Proxy to the Autoscaler](README.md#context).

### Integration

1. **Autoscaler multitenancy** will allow the autoscaler to remain "always on".  It will also reduce the overhead of running 1 single-tenant autoscaler pod per revision.
2. **Consume custom metrics API** as an abstraction to allow pluggability.  This may require another metrics aggregation component to get the queue-proxy produced concurrency metrics behind a custom metrics API.  It will also allow autoscaling based on Prometheus metrics through an adapter.  A good acceptance criteria is the ability to plug in the vanilla Horizontal Pod Autoscaler (HPA) in lieu of the Elafros autoscaler (minus scale-to-zero capabilities).
3. **Autoscale queue-based workloads** in addition to request/reply workloads.  The first use-case is the integration of Riff autoscaling into the multitenant autoscaler.  The autoscaler must be able to select the appropriate strategy for scaling the revision.

## What We Are Not Doing

These are things we are explicitly leaving off the roadmap.  But we might do exploratory work to set them up for later development.  Most of these are related to Design Goal #3: *make everything better*.

1. **Use Envoy for single-threading** instead of using the Queue Proxy to enforce serialization of requests to the application container.  This only applies in single-threaded mode.  It would allow us to remove the Queue Proxy entirely.  But it would probably require feature work in Envoy/Istio.
