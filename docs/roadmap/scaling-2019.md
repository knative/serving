# 2019 Autoscaling Roadmap

This is what we hope to accomplish in 2019.

## 2019 Goals

### Sub-Second Cold Start

Serverless is only as good as the illusion it sustains. Throwing some code into a "serverless" framework, the expectation is that it will just be running when it needs to, with as many resources as necessary. Including zero. But to realize that magical threshold and maintain the illusion of serverless, the code must come back as if it was never gone. The exact latency requirement of "as if it was never gone" will vary from use-case to use-case. But generally less than one second is a good start.

Right now cold-starts are between 10 and 15 seconds which is an order of magnitude too slow. The time is spent starting the pod, waiting for Envoy to start and telling all nodes how to reach the pod through the Kubernetes Service. Without the Istio mesh (just routing request to individual pods as they come up) still takes about 4 seconds.

We've poked at this problem in 2018 ([#1297](https://github.com/knative/serving/issues/1297)) but haven't been able to make significant progress. This area requires some dedicated effort to:

1. identify and programatically capture sources of cold-start latency at all levels of the stack ([#2495](https://github.com/knative/serving/issues/2495))
2. chase down the low hanging fruit (e.g. [#2659](https://github.com/knative/serving/issues/2659))
3. architect solutions to larger chunks of cold-start latency

**Our goal is to achieve sub-second average cold-starts by the end of the year.**

* POC: Greg Haynes (IBM)
* Github: [Project 8](https://github.com/knative/serving/projects/8)

### Overload Handling

Knative Serving provides concurrency controls to limit the number of requests a container must handle simultaneously. Additionally, each pod has a queue for holding requests when the container concurrency limit has been reached. When the pod-level queue overflows, subsequent request are rejected with 503 "overload".

This is desirable to protect the Pod from being overloaded. But in the aggregate the behavior is not ideal for situations when autoscaling needs some time to react to sudden increases in request load (e.g. scale-from-zero).

The goal of Overload Handling is to enqueue requests at a revision-level. Scale-from-zero should not overload if autoscaling can react in a reasonable amount of time to provide additional pods. When new pods come online, they should be able to take load for the existing pods. Even when scaled above zero, brief spikes of overload should be handled by enqueuing requests at a revision-level. The depth of the revision-level queue should also be configurable because even the Revision as a whole needs to guard against overload.

The overall problem touches on both Networking and Autoscaling, two different working groups. Much of the overload handling will be implemented in the Activator, which is a part of ingress. So this project is shared jointly between the two working groups.

**Our goal is for a Revision with 1 Pod and a container concurrency of 1 to handle 1000 requests arriving simultaneously within 30 seconds with no errors, where each request sleeps for 100ms.**

E.g.
```yaml
apiVersion: serving.knative.dev/v1alpha1
kind: Service
metadata:
  name: overload-test
spec:
  runLatest:
    configuration:
      revisionTemplate:
        metadata:
          annotations:
            autoscaling.knative.dev/minScale: "1"
            autoscaling.knative.dev/maxScale: "10"
        spec:
          containerConcurrency: 1
          container:
            image: gcr.io/joe-does-knative/sleep:latest
            env:
            - name: SLEEP_TIME
              value: "100ms"
```
```bash
for i in `seq 1 1000`; do curl http://$MY_IP & done
```

This verifies that 1) we can handle all requests in an overload without error and 2) all the requests don't land on a single pod, which would take 100 sec.

* POC: Vadim Raskin (IBM)
* Github: [Project 7](https://github.com/knative/serving/projects/7)

### Autoscaler Availability

Because Knative scales to zero, autoscaling is in the critical-path for serving requests. If the autoscaler isn't available when an idle Revision receives a request, that request will not be served. Other components such as the Activator are in this situation too. But they are more stateless and so can be scaled horizontally relatively easily. For example, any Activator can proxy any request for any Revision. All it has to do is send a messge to the Autoscaler and then wait for a Pod to show up. Then it proxies the request and is taken back out of the serving path.

However the Autoscaler process is more stateful. It maintains request statistics over a window of time. And it must process data from the Revision Pods continously to maintain that window of data. It is part of the running system all the time, not just when scaled to zero. As the number of Revisions increase and the number of Pods in each Revision increases, the CPU and memory requirements will exceed that available to a single process. So some sharding is necessary.

**Our goal is to shard Revisions across Autoscaler replicas (e.g. 2x the Replica count means 1/2 the load on each Autoscaler). And for autoscaling to be unaffected by an individual Autoscaler termination.**

* POC: Kenny Leung (Google)
* Github: [Project 19](https://github.com/knative/serving/projects/19)

### Streaming Autoscaling

In addition to being always available, Web applications are expected to be responsive. Long-lived connections and streaming protocols like Websockets and HTTP2 are essential. Knative Serving accepts HTTP2 connections. And will serve requests multiplexed within the connection. But the autoscaling subsystem doesn't quite know what to do with those connections. It sees each connection as continuous load on the system and so will autoscale accordingly.

But the actual load is in the stream within the connection. So the metrics reported to the Autoscaler should be based on the number of concurrent *streams*. This requires some work in the Queue proxy to crack open the connection and emit stream metrics.

Additionally, concurrency limits should be applied to streams, not connections. So containers which can handle only one request at at time should still be able to serve HTTP2. The Queue proxy will just allow one stream through at a time.

**Our goal is 1) to support HTTP2 end-to-end while scaling on concurrent streams and in this mode 2) enforce concurrency limits on streams (not connections).**

* POC: Markus Thömmes (Red Hat)
* Github: [Project 16](https://github.com/knative/serving/projects/16)

### Pluggability and HPA

This is work remaining from 2018 to add CPU-based autoscaling to Knative and provide an extension point for further customizing the autoscaling sub-system. Remaining work includes:

1. metrics pipeline relayering to scrape metrics from Pods ([#1927](https://github.com/knative/serving/issues/1927))
2. adding a `window` annotation to allow for further customization of the KPA autoscaler ([#2909](https://github.com/knative/serving/issues/2909))
3. implementing scale-to-zero for CPU-scaled workloads ([#3064](https://github.com/knative/serving/issues/3064))

**Our goal is to have a cleanly-layered, extensible autoscaling sub-system which fully supports concurrency and CPU metrics (including scale-to-zero).**

* POC: Yanwei Guo (Google)
* Github: [Project 11](https://github.com/knative/serving/projects/11)

### Vertical Pod Autoscaling Beta

Another dimension of the serverless illusion is running code efficiently. Knative has default resources request. And it supports resource requests and limits from the user. But if the user doesn't want to spend their time "tuning" resources, which is a very "serverful" way to spend one's time, Knative should be able to just "figure it out". That is Vertical Pod Autoscaling (VPA).

Knative previously integrated with VPA Alpha. Now it needs to reintegrate with VPA Beta. In addition to creating VPA resources for each Revision, we need to do a little bookkeeping for the unique requirements of serverless workloads. For example, the window for VPA recommendations is 2 weeks. But a serverless function might be invoked once per year (e.g. when the fire alarm gets pulled). The Pods should come back with the correct resource requests and limits. The way VPA is architected, it "injects" the correct recommendations via mutating webhook. It will decline to update resources requests after 2 weeks of inactivity and the Revision would fall back to defaults. Knative needs to remember what that recommendation was and make sure new Pods start at the right levels.

Additionally, the next Revision should learn from the previous. But it must not taint the previous Revision's state. For example, when a Service is in runLatest mode, the next Revision should start from the resource recommendations of the previous. Then VPA will apply learning on top of that to adjust for changes in the application behavior. However if the next Revision goes crazy because of bad recommendations, a quick rollback to the previous should pick up the good ones. Again, this requires a little bit of bookkeeping in Knative.

**Our goal is support VPA enabled per-revision with 1) revision-to-revision inheritance to recommendations (when appropriate) and 2) safe rollback to previous recommendations when rolling back to previous Revisions.**

* POC: Joseph Burnett (Google)
* Github: [Project 18](https://github.com/knative/serving/projects/18)
