# Autoscaling

## Behavior

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
