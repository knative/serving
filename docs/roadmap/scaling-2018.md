# 2018 Autoscaling Roadmap

This is what we hope to accomplish in 2018.

## References

[Autoscaling Design Goals](README.md#design-goals):

1. _Make it fast_
2. _Make it light_
3. _Make everything better_

In 2018 we will focus primarily on making autoscaling correct, fast and light.

## Areas of Interest and Requirements

1. **Correctness**. When scaling from 0-to-1, 1-to-N and back down, error rates
   must not increase. We must have visibility of correctness over time at small
   and large scales.
2. **Performance**. When scaling from 1-to-N and back down, autoscaling must
   maintain reasonable latency. The Knative Serving implementation of
   autoscaling must be competitive in its ability to serve variable load.
3. **Scale to zero**. Idle ([Reserve](README.md#behavior)) Revisions must cost
   nothing. Reserve Revisions must serve the first request in 1 second or less.
4. **Development**. Autoscaler development must follow a clear roadmap. Getting
   started as a developer must be easy and the team must scale horizontally.
5. **Integration**. Autoscaler should be pluggable and support multiple
   strategies and workloads.

### Correctness

1. **Write autoscaler end-to-end tests** to cover low-scale regressions,
   runnable by individual developers before checkin.
   ([#420](https://github.com/knative/serving/issues/420))
2. **Test error rates at high scale** to cover regressions at larger scales
   (~1000 QPS and ~1000 clients).
   ([#421](https://github.com/knative/serving/issues/421))
3. **Test error rates around idle states** to cover various scale-to-zero edge
   cases. ([#422](https://github.com/knative/serving/issues/422))

### Performance

1. **Establish canonical load test scenarios** to prove autoscaler performance
   and guide development. We need to establish the code to run, the request load
   to generate, and the performance expected. This will tell us where we need to
   improve.
2. **Reproducable load tests** which can be run by anyone with minimal setup.
   These must be transparent and easy to run. They must be meaningful tests
   which prove autoscaler performance.
   ([#424](https://github.com/knative/serving/pull/424))
3. **Vertical pod autoscaling** to allow revisions to adapt the differing
   requirements of user's code. Rather than applying the same resource requests
   to all revision deployments, use vertical pod autoscaler (or something) to
   adjust the resources required. Resource requirements for one revision should
   be inherited by the next revision so there isn't always a learning period
   with new revisions.

### Scale to Zero

1. **Implement scale to zero**. Revisions should autoamtically scale to zero
   after a period of inactivity. And scale back up from zero to one with the
   first request. The first requests should succeed (even if latency is high)
   and then latency should return to normal levels once the revision is active.
2. **Reduce Reserve Revision start time** from 4 seconds to 1 second or less.
   Maybe we can keep some resources around like the Replica Set or pre-cache
   images so that Pods are spun up faster.

### Development

1. **Decouple autoscaling from revision controller** to allow the two to evolve
   separately. The revision controller should not be setting up the autoscaler
   deployment and service.

### Integration

1. **Autoscaler multitenancy** will allow the autoscaler to remain "always on".
   It will also reduce the overhead of running one single-tenant autoscaler pod
   per revision.
2. **Consume custom metrics API** as an abstraction to allow pluggability. This
   may require another metrics aggregation component to get the queue-proxy
   produced concurrency metrics behind a custom metrics API. It will also allow
   autoscaling based on Prometheus metrics through an adapter. A good acceptance
   criteria is the ability to plug in the vanilla Horizontal Pod Autoscaler
   (HPA) in lieu of the Knative Serving autoscaler (minus scale-to-zero
   capabilities).
3. **Autoscale queue-based workloads** in addition to request/reply workloads.
   The first use-case is the integration of Riff autoscaling into the
   multitenant autoscaler. The autoscaler must be able to select the appropriate
   strategy for scaling the revision.

## What We Are Not Doing Yet

These are things we are explicitly leaving off the roadmap. But we might do
exploratory work to set them up for later development. Most of these are related
to Design Goal #3: _make everything better_.

1. **Use Envoy for single-threading** instead of using the Queue Proxy to
   enforce serialization of requests to the application container. This only
   applies in single-threaded mode. It would allow us to remove the Queue Proxy
   entirely. But it would probably require feature work in Envoy/Istio.
2. **Remove metrics reporting from the Queue Proxy** in order to rely on a
   common, Knative Serving metrics pipeline. This could mean polling the Pods to
   get the same metrics as are reported to Prometheus. Or going to Prometheus to
   get the metrics it has aggregated. It means removing the metrics push from
   the [Queue Proxy to the Autoscaler](README.md#context).
3. **[Slow Brain](README.md#slow-brain--fast-brain) implementation** to
   automatically adjust target concurrency to the application's behavior.
   Instead, we can rely on vertical pod autoscaling for now to size the pod to
   an average of one request at a time.
