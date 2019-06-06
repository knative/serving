# 2019 Autoscaling Roadmap

This is what we hope to accomplish in 2019.

## Performance

### Tests and reliable reporting

As an overarching goal, we want all aspects of our performance continuously
measured and reliably reported. All of the following aspects will include
in-depth testing and reporting to make sure that advancements are reproducible
on the CI systems and to avoid unwanted regressions.

**Goal**: All relevant performance numbers are tracked and reported.

**Project**: No seperate project for now.

### Sub-Second Cold Start

As a serverless framework, Knative should only run code when it needs to.
Including scaling to zero when the Revision is not being used. However the
Revison must also come back quickly, otherwise the illusion of "serverless" is
broken--it must seem as if it was always there. Generally less than one second
is a good start.

Today cold-starts are between 10 and 15 seconds which is an order of magnitude
too slow. The time is spent starting the pod, waiting for Envoy to initialize,
and setting up routing. Without the Istio mesh (just routing request to
individual pods as they come up) still takes about 4 seconds. We've poked at
this problem in 2018 ([#1297](https://github.com/knative/serving/issues/1297))
but haven't made significant progress. This area requires some dedicated effort.

**Goal**: Achieve sub-second average cold-starts of disk-warm Revisions.

**Project**: [Project 8](https://github.com/knative/serving/projects/8)

### Overload Handling

Knative Serving provides concurrency controls to limit the number of requests a
container can handle simultaneously. Additionally, each pod has a queue for
holding requests when the container concurrency limit has been reached. When the
pod-level queue overflows, subsequent request are rejected with 503 "overload".

This is desirable to protect the pod from being overloaded. But the aggregate
behavior is not ideal for situations when autoscaling needs some time to react
to sudden increases in request load. This could happen when the Revision is
scaled to zero or when the Revision is already running some pods, but not nearly
enough.

The goal of Overload Handling is to enqueue requests at a Revision-level.
Scale-from-zero should not overload if autoscaling can react in a reasonable
amount of time to provide additional pods. When new pods come online, they
should be able to take load from the existing pods. Even when scaled above zero,
brief spikes of overload should be handled by enqueuing requests at a
Revision-level. The depth of the Revision-level queue should also be
configurable because even the Revision as a whole needs to guard against
overload.

The overall problem touches on both networking and autoscaling, two different
working groups. Much of the overload handling will be implemented in the
Activator, which is a part of ingress. So this project is shared jointly between
the two working groups.

**Goal**: Requests can be enqueued at the Revision-level in response to high
load.

**Project**: [Project 7](https://github.com/knative/serving/projects/7)

## Reliability

### Autoscaling Availabilty

Because Knative scales to zero, the autoscaling system is in the critical-path
for serving requests. If the Autoscaler or Activator isn't available when an
idle Revision receives a request, that request will not be served. The Activator
is stateless and can be easily scaled horizontally. Any Activator pod can proxy
any request for any Revision. But the Autoscaler pod is stateful. It maintains
request statistics over a window of time. Moreover, the relationship between
Activator and Autoscaler is N:1 currently because of how the Activator pushes
metrics into the Autoscaler via a Websocket connection.

We need a way for autoscaling to have higher availability than that of a single
pod. When an Autoscaler pod fails, another one should take over, quickly. And
the new Autoscaler pod should make equivalent scaling decisions.

**Goal**: The autoscaling is more highly available than a single pod.

**Project**: TBD

### Autoscaling Scalability

The Autoscaler process maintains pod metric data points over a window of time
and calculates average concurrency every 2 seconds. As the number and size of
Revisions deployed to a cluster increases, so does the load on the Autoscaler.

We need some way to have sub-linear load on a given Autoscaler pod as the
Revision count increases. This could be a sharding scheme or simply deploying
separate Autoscalers per namespace.

**Goal**: The autoscaling system can scale sub-linearly with the number of
Revisions and number of Revision pods.

**Project**: TBD

## Extendability

### Pluggability

It is possible to replace the entire autoscaling system by implementing an
alternative PodAutoscaler reconciler (see the
[Yolo controller](https://github.com/josephburnett/kubecon18)). However that
requires collecting metrics, running an autoscaling process, and actuating the
recommendations.

We should be able to swap out smaller pieces of the autoscaling system. For
example, the HPA should be able to make use of the metrics that Knative
collects.

**Goal**: The autoscaling decider and metrics collection components can be
replaced independently.

**Project**: TBD

### HPA Integration

The current Knative integration with K8s HPA only supports CPU autoscaling.
However it should be able to scale on concurrency as well. Ultimately, the HPA
may be able to replace the Knative Autoscaler (KPA) entirely (see
["make everything better"](https://github.com/knative/serving/blob/master/docs/roadmap/scaling-2018.md#references)).
Additionally, HPA should be able to scale on user-provided custom metrics as
well.

**Goal**: Knative HPA-class PodAutoscalers support concurrency autoscaling.

**Project**: TBD

## User Experience

### Migrating Kubernetes Deployments to Knative

We need documentation and examples to help Kubernetes users with existing
Kubernetes Deployments migrate some of those to Knative to take advantage of
request-based autoscaling and scale-to-zero.

**Goal**: Increase Knative adoption by making migration from Kubernetes
Deployments simple.

## What We Are Not Doing Yet

### Removing the Queue Proxy Sidecar

There are two sidecars injected into Knative pods, Envoy and the Queue Proxy.
The queue-proxy sidecar is where we put everything we wish Envoy/Istio could do,
but doesn't yet. For example, enforcing single-threaded request or reporting
concurrency metrics in the way we want. Ultimately we should push these features
upstream and get rid of the queue-proxy sidecar.

However we're not doing that yet because the requirements haven't stablized
enough yet. And it's still useful to have a component to innovate within.

See
[2018 What We Are Not Doing Yet](https://github.com/knative/serving/blob/master/docs/roadmap/scaling-2018.md#what-we-are-not-doing-yet)

### Vertical Pod Autoscaling Beta

A serverless system should be able to run code efficiently. Knative has default
resources request and it supports resource requests and limits from the user.
But if the user doesn't want to spend their time "tuning" resources (which is
very "serverful") then Knative should be able to just "figure it out". That is
Vertical Pod Autoscaling (VPA).

Knative
[previously integrated with VPA Alpha](https://github.com/knative/serving/issues/839#issuecomment-389387311).
Now it needs to reintegrate with VPA Beta. In addition to creating VPA resources
for each Revision, we need to do a little bookkeeping for the unique
requirements of serverless workloads. For example, the window for VPA
recommendations is 2 weeks. But a serverless function might be invoked once per
year (e.g. when the fire alarm gets pulled). The pods should come back with the
correct resource requests and limits. The way VPA is architected, it "injects"
the correct recommendations via mutating webhook. It will decline to update
resources requests after 2 weeks of inactivity and the Revision would fall back
to defaults. Knative needs to remember what that recommendation was and make
sure new pods start at the right levels.

Additionally, the next Revision should learn from the previous. But it must not
taint the previous Revision's state. For example, when a Service is in runLatest
mode, the next Revision should start from the resource recommendations of the
previous. Then VPA will apply learning on top of that to adjust for changes in
the application behavior. However if the next Revision goes crazy because of bad
recommendations, a quick rollback to the previous should pick up the good ones.
Again, this requires a little bit of bookkeeping in Knative.

**Project**: [Project 18](https://github.com/knative/serving/projects/18)
