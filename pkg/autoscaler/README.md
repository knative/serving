# Autoscaling

Elafros Revisions are automatically scaled up and down according incoming traffic.

## Behavior

When a Revision is actively serving requests it will increase and descrease the number of Pods to maintain the desired average concurrent clients per process.  When requests are longer being served, the Revision will be scaled down to 0 Pods.  When the first request arrives, the Revision will be scaled back up again.

## Implementation

The Revision has three autoscaling states which are
1. Active when the Revision is actively serving requests,
2. Reserve when the Revision is scaled down to 0 Pods but is still in service, and
3. Retired when the Revision will no longer recieve traffic.

In the Active state, each Revision has a Deployment which maintains the desired number of Pods.  It also has an Autoscaler which watches traffic metrics and adjusts the Deployment's desired number of pods up and down.  Each Pod reports its current QPS and number of current clients each second to the Autoscaler.

In the Reserve state, the Revision has no scheduled pods.  The Istio route rule for the Revision points to the singleton Activator which will catch traffic for all Reserve Revisions.  When a request arrives to the Activator, it first flips the desired state of the Revision to Active.  Then it watches for the first available Pod.  All pending and subsequent requests are then forwarded to the first Pod.  As the Revision becomes active, the Istio route rules will be updated to route traffic away from the Activator and onto the Pods directly.

In the Retired state, the Revision has provisioned resources.  No requests will be served for the Revision.

## Context 

```
        +---------+
        | INGRESS |------------------+
        +---------+                  |
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

## Pod Autoscaling

## Cluster Autoscaling
