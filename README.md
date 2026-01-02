# Implemented Knative Features
We design and implement additional features for Knative Serving to provide architectural support for WASM superpods.
The implementation is based on Knative Serving v1.15.2.

## Superpod Feature Gate

The Superpod feature gate is `features.knative.dev/superpod`.
Once enabled, QueueProxy and Activator track the concurrent memory request of all requests that are running and queued.

Instead of using the default proxy handler, the `sp_hanlder` is implemented in QueueProxy, which  tracks the memory request of function requests for superpods by reading the `Memory-Request` attribute from the request HTTP header.
Default memory request of 200MB is used if `Memory-Request` is missing.
Also, Activator tracks memory request in the same way when the ksvc is in the Proxy mode.


Example configuration
```
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: example-configuration
spec:
  template:
    metadata:
      annotations:
        features.knative.dev/superpod: "Enabled"
    spec:
      containers:
        - image: harbor.lincy.dev/funinsight/go-func-nc:latest
          ports:
            - containerPort: 8080
```

## Autoscaler Scaling Metrics
The metric configuration `autoscaling.knative.dev/metric` defines which metric type is watched by the Autoscaler.
The default KPA Autoscaler supports the concurrency and rps metrics.
The memory metric support is implemented in KPA Autoscaler for superpod autoscaling based on memory requests.

Example configuration
```
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: example-configuration
spec:
  template:
    metadata:
      annotations:
        autoscaling.knative.dev/metric: "memory"
        features.knative.dev/superpod: "Enabled"
    spec:
      containers:
        - image: harbor.lincy.dev/funinsight/go-func-nc:latest
          ports:
            - containerPort: 8080
```

## Autoscaling on Memory Request
This section describes configurable parameters and scaling behaviors for autoscaling on memory request, namely when the superpod feature gate is enabled.
### Targets
The scaling target is a soft limit.
Configuring a target provides the Autoscaler with a value that it tries to maintain for the configured metric for a revision.
The per-revision annotation key for configuring scaling targets is `autoscaling.knative.dev/target`.
If target is not set, `concurrentResourceRequest` will be used as the target.

If the target is explicitly set in the annotation, it will be used as the scaling target.
If the target is not set, the Autoscaler will use the `concurrentResourceRequest * TargetUtilization` as the scaling target.

### Target utilization
This value specifies what percentage of the previously specified target should actually be targeted by the Autoscaler. This is also known as specifying the hotness at which a replica runs, which causes the Autoscaler to scale up before the defined hard limit is reached.
TargetUtilization is a percentage in the 1 <= TU <= 100 range.
The per-revision annotation key is `autoscaling.knative.dev/target-utilization-percentage`.
The default value of 70 will be used if not set.

Note: If the target annotation is set, the target utilization will be ignored.

### Concurrent resource request and queue depth
The concurrent resource request is a hard limit for the user container (e.g., user container), which defines the maximum amount of resource requests (e.g., memory request in MB) that are allowed to flow to the replica (i.e., superpod) at any one time.
This value is usually greater than the actual resource limit in case of resource overcommitment.
The per-revision annotation key for configuring concurrent resource requests is `concurrentResourceRequest`.
The default value of 8000 will be used if not set.


The queue depth is a hard limit for the resource request of all in-flight (i.e., requests running in the user container and those waiting in the queue proxy) requests. Further incoming requests will be immediately throttled if this hard limit is reached.
The per-revision annotation key for configuring the queue depth is `queueDepthResourceUnits`.
The default value of 16000 will be used if not set.
The `queueDepthResourceUnits` value should be greater than or equal to `concurrentResourceRequest`.


Note: `concurrentResourceRequest` and `queueDepthResourceUnits` also have implications on the throttling and breaker mechanisms in the Activator and QueueProxy, in which they are used to determine the maximum resource request units that can be queued.

Example configuration
```
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: example-configuration
spec:
  template:
    metadata:
      annotations:
        autoscaling.knative.dev/metric: "memory"
        features.knative.dev/superpod: "Enabled"
    spec:
      containers:
        - image: harbor.lincy.dev/funinsight/go-func-nc:latest
          ports:
            - containerPort: 8080
      concurrentResourceRequest: 50000
      queueDepthResourceUnits: 150000
```

### Resource Utilization Threshold
The resource utilization threshold specifies the maximum resource utilization for admitting new requests to the user container (i.e., superpod).

If in+resourceUnits > capacity || s.metricValue.Load().(spmetricserver.SuperPodMetrics).CpuUtilization > s.resourceUtilizationThreshold, the request will be queued in the QueueProxy.

Example configuration
```
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: example-configuration
spec:
  template:
    metadata:
      annotations:
        autoscaling.knative.dev/metric: "memory"
        features.knative.dev/superpod: "Enabled"
    spec:
      containers:
        - image: harbor.lincy.dev/funinsight/go-func-nc:latest
          ports:
            - containerPort: 8080
      resourceUtilizationThreshold: 6.5
```

### Target burst capacity
Target burst capacity determines the size of traffic burst a Knative application can handle without buffering.
If a traffic burst is too large for the application to handle, the Activator service will be placed in the request path to protect the revision and optimize request load balancing.
In case of autoscaling on memory request, the size of the traffic is determined by the memory request amount.
The per-revision annotation key for configuring target burst capacity is `autoscaling.knative.dev/target-burst-capacity`.
Default value of 200 (i.e., 200MB) will be used if not set.

## Excess Burst Capacity (EBC)
The excess burst capacity (EBC) determines if there is enough capacity to serve the traffic.

EBC is defined as
```
EBC = TotalCapacity - ObservedPanicValue - TargetBurstCapacity
```
In case of autoscaling on memory request (the superpod feature gate is enabled), TotalCapacity is calculated as
```
TotalCapacity = #ReadyPods * Target
```
If EBC >=0, the current capacity is considered enough to serve the traffic, and the activator will be removed from the data path.

### Example configuration
```
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: example-configuration
spec:
  template:
    metadata:
      annotations:
        features.knative.dev/superpod: "Enabled"
        autoscaling.knative.dev/metric: "memory"
        autoscaling.knative.dev/target: "10000"
        autoscaling.knative.dev/target-burst-capacity: "2000"
    spec:
      containers:
        - image: harbor.lincy.dev/funinsight/go-func-nc:latest
          ports:
            - containerPort: 8080
      concurrentResourceRequest: 12000
      containerConcurrency: 0
```

## Request Queueing Support
### Resource Breaker
ResourceBreaker is a component in QueueProxy that enforces a concurrent resource request (e.g., memory) limit on the execution of a function in superpods. 
It also maintains a queue of function executions in excess of `ConcurrentResourceRequest`.
Function calls are queued if  `ConcurrentResourceRequest` < `current in-flight resource requests + new resource request` <= `QueueDepthUnits`.
Function calls are failed immediately if `current in-flight resource requests + new resource request` > `QueueDepthUnits`

The resource breaker is configured with the following parameters:
```
QueueDepthUnits = 1 * QueueDepthResourceUnits
MaxConcurrencyUnits = 1 * ConcurrentResourceRequest
InitialCapacityUnits = 1 * ConcurrentResourceRequest
totalSlots =  QueueDepthUnits + MaxConcurrencyUnits
```

### Throttler
ResourceThrottler
