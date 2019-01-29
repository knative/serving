# 2019 Autoscaling Roadmap

This is what we hope to accomplish in 2019.

## 2019 Goals

### Sub-Second Cold Start

Serverless is only as good as the illusion we create. Throwing some code into a "serverless" framework, the expectation is that it will just be running when it needs to, with as many resources as necessary. Including zero. But to realize that magical threshold and maintain the illusion of serverless, the code must come back as if it was never gone. The exact latency requirement of "as if it was never gone" will vary from use-case to use-case. But generally less than one second is a good start.

Right now cold-starts are between 10 and 15 seconds which is an order of magnitude too slow. The time is spent starting the pod, waiting for Envoy to start and telling all nodes how to reach the pod through the Kubernetes Service. Without the Istio mesh (just routing request to individual pods as they come up) still takes about 4 seconds.

This area requires some dedicated effort to:

1. identify and programatically capture sources of cold-start latency at all levels of the stack ([#2495](https://github.com/knative/serving/issues/2495))
2. chase down the low hanging fruit (e.g. [#2659](https://github.com/knative/serving/issues/2659))
3. architect solutions to larger chunks of cold-start latency

The goal is to achieve sub-second average cold-starts by the end of the year.

### Overload Handling

Knative Serving provides concurrency controls to limit the number of requests a container must handle simultaneously. Additionally, each pod has a queue for holding requests when the container concurrency limit has been reached. When the pod-level queue has been filled to capacity, subsequent request are rejected with 503 "overload".

This is desireable to protect the Pod from being overloaded.  But in the aggregate the behavior is not ideal for situations when autoscaling needs some time to react to sudden increases in request load (e.g. scale-from-zero).

The goal of Overload Handling is to enqueue requests at a revision-level. Scale-from-zero should not overload if autoscaling can react in a reasonable amount of time to provide additional pods. When new pods come online, they should be able to take load for the existing pods. Even when scaled above zero, brief spikes of overload should be handled by enqueuing requests at a revision-level. The depth of the revision-level queue should also be configurable because even the Revision as a whole needs to guard against overload.

This overall problem is closely related to both Networking and Autoscaling, two different working groups. Much of the overload handling will be implemented in the Activator, which is a component most closely related to ingress, part of the Networking WG's charter. So this project is shared jointly between the two working groups.

### Autoscaler Availability

Because Knative scales to zero, autoscaling is in the critical-path for serving requests. If the autoscaler isn't available when an idle Revision receives a request, that request will not be served. Other components such as the Activator are in this situation too. But they are more stateless and so can be scaled horizontally relatively easily. For example, any Activator can proxy a request for any Revision. All it has to do is send a messge to the Autoscaler and then wait for a Pod to send the request to. Then it proxies the request and it take back out of the serving path.

However the Autoscaler process is more stateful. It maintains request statistics over a window of time. And it must process data from the Revision Pods continously to maintain that window of data. It is part of the system all the time, not just when scaled to zero. As the number of Revisions and the number of Pods in each Revision increases, the CPU and memory requirements will exceed that available to a single process. Some sharding is necessary.

### Streaming Autoscaling

In addition to being always available, Web applications are expected to be responsive and connected. Continuously connected and streaming protocols like Websockets and HTTP2 are essential to a modern application.

Knative Serving accepts HTTP2 connections. And will serve requests multiplexed within the connection. But the autoscaling subsystem doesn't quite know what to do with those connections. It sees each connection as continuous load on the system and so will autoscale accordingly.

But the actual load is in the stream within the connection. So the metrics reported to the Autoscaler should be based on the number of concurrent **streams**. This requires some work in the Queue proxy to crack open the connection and emit stream metrics.

Additionally, concurrency limits should be applied to streams, not connections. So containers which can handle only one request at at time should still be able to serve HTTP2. The Queue proxy will just allow one stream through at a time.

### Vertical Pod Autoscaling Beta

Another aspect of the "serverless" illusion is figure out what code needs to run and running it efficiently. Knative has default resources request. And it supports resource requests and limit from the user. But if the user doesn't want to spend their time "tuning" resources, which is a very "serverful" way to spend your time, Vertical Pod Autoscaling (VPA) is needed.

Knative previously integrated with VPA Alpha. Now it needs to reintegrate with VPA Beta. In addition to creating VPA resources for each Revision, we need to do a little bookkeeping for the unique requirements of serverless workloads. For example, the window for VPA recommendations is 2 weeks. But a serverless function might be invoked once per year (e.g. when the fire alarm gets pulled). The Pods should come back with the correct resource requests and limits. The way VPA is architected, it "injects" the correct recommendations and so it would fail to use the right resources after 2 weeks of inactivity. Knative needs to remember what that recommendation was and make sure new Pods start at the right levels.

Additionally, one Revision should learn from the previous. But it must not taint the previous Revision's state. For example, when a Service is in runLatest mode, the next Revision should start from the resource requests of the previous. Then VPA will apply learning on top of that to adjust for changes in the application behavior. However if the next Revision goes crazy because of bad recommendations, a quick rollback to the previous should pick up the good ones. Again, this requires a little bit of bookkeeping in Knative.
