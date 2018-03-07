# Autoscaling

Elafros Revisions are automatically scaled up and down according incoming traffic.

## Behavior

When a Revision is actively serving requests it will increase and descrease the number of Pods to maintain the desired average concurrent clients per process.  When requests are no longer being served, the Revision will be scaled down to 0 Pods.  When the first request arrives, the Revision will be scaled back up again.

The Revision has three autoscaling states which are
1. **Active** when the Revision is actively serving requests,
2. **Reserve** when the Revision is scaled down to 0 Pods but is still in service, and
3. **Retired** when the Revision will no longer recieve traffic.

In the Active state, each Revision has a Deployment which maintains the desired number of Pods.  It also has an Autoscaler which watches traffic metrics and adjusts the Deployment's desired number of pods up and down.  Each Pod reports its current QPS and number of current clients each second to the Autoscaler.

In the Reserve state, the Revision has no scheduled pods.  The Istio route rule for the Revision points to the singleton Activator which will catch traffic for all Reserve Revisions.  When a request arrives to the Activator, it first flips the desired state of the Revision to Active.  Then it watches for the first available Pod.  All pending and subsequent requests are then forwarded to the first Pod.  As the Revision becomes active, the Istio route rules will be updated to route traffic away from the Activator and onto the Pods directly.

In the Retired state, the Revision has provisioned resources.  No requests will be served for the Revision.

## Context 

```
   +---------------------+
   | ROUTE               |
   |                     |
   |   +-------------+   |
   |   | Istio Route |---------------+
   |   +-------------+   |           |
   |         |           |           |
   +---------|-----------+           |
             |                       |
             |                       |
             | inactive              | active
             |  route                | route
             |                       |
             |                       |
             |                +------|---------------------------------+
             V         watch  |      V                                 |
       +-----------+   first  |   +- ----+  create   +------------+    |
       | Activator |------------->| Pods |<----------| Deployment |    |
       +-----------+          |   +------+           +------------+    |
             |                |       |                     ^          |
             |   activate     |       |                     | resize   |
             +--------------->|       |                     |          |
                              |       |    metrics    +------------+   |
                              |       +-------------->| Autoscaler |   |
                              |                       +------------+   |
                              | REVISION                               |
                              +----------------------------------------+
                              
```

## Design Goals

1. **Make if fast**.  Revisions should be able to scale from 0 to 1000 concurrent writers in 30 seconds or less.
2. **Make it light**.  Wherever possible the system should be able to figure out the right thing to do.
3. **Make everything better**.  Creating custom components is a short-term strategy to get something working now.  The long-term strategy is to make the underlying components better so that custom code can be replaced with configuration.  E.g. Autoscale should be replaced with the K8s Horizontal Pod Autoscaler and Custom Metrics.

## Implementation

There is a proxy on in the Elafros Pods (`queue-proxy`) who is responsible enforcing request queue parameters, and reporting concurrent client metrics to the Autoscaler.  If we can get rid of this and just use Envoy, that would be great (see Design Goal #3).  The Elafros controller injects the identity of the Revision into the Pod queue proxy environment variables.  When the queue proxy wakes up, it find the Autoscaler for the revision and establishes a websocket connection.  Every 1 second, the queue proxy pushes a gob serialized struct with the currently observed number of concurrent clients.

The Autoscaler is also given the identity of the Revision through environment variables.  When it wakes up, it starts a websocket-enabled http server.  Queue proxies start sending their metrics to the Autoscaler and it maintains a 60-second sliding window of data points.
