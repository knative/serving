# Autoscaling

There are three main components to the autoscaling system: autoscaler, activator and queue-proxy.

## Queue-proxy
Whenever a user deploys an application, it is injected with a queue-proxy sidecar container. This queue-proxy does the following:
 - Any request coming to the application user-container will go to the queue-proxy and then be routed to the user-container
 - Any readiness and liveness probes defined by the user will be replaced with queue-proxy probes externally to the pod while the user-containers probe endpoints will only be accessed by the proxy.
 - Makes sure that no more than the ‘defined container concurrency’ requests reach the application's instance at once by queueing other requests. For example if a revision defines a concurrency limit of 5, the queue-proxy makes sure that no more than 5 requests reach the application's instance at once. If there are more requests being sent to it than that, it will queue them locally.
 - Collects metrics about the load of requests the application  container receives and reports the `average concurrency` and `requests per second` on a separate port.

## Activator
The activator is mainly involved around scale to/from zero and in capacity aware load balancing. When a revision is scaled to zero instances, the activator will be put into the data path instead of revision's instances. If requests hit this revision, the activator buffers these requests, pokes the autoscaler with metrics and holds the requests until instances of the application appear.

The Activator being in the data path can be changed according to the target-burst-capacity configuration value.

#### target-burst-capacity
Depending on the value used for this configuration parameters, the activator can do the following:
 - If `target-burst-capacity=0`, the Activator is only added to the request path during scale from zero scenarios, and ingress load balancing will be applied.
 - If `target-burst-capacity=-1`, the Activator is always in the request path, regardless of the revision size.
 - If `target-burst-capacity=another integer`, the Activator will be in the path when the revision is scaling up from zero.

When the application is ready to receive the requests buffered in the activator, it effectively acts as a loadbalancer and distributes the load across all the pods as they become available in a way that doesn't overload them with regards to their concurrency settings. The type of load balancing depends of the specification of the Service.

#### load balancing types
Based on the container concurrency configuration of the revision, the algorithm for load balancing may differ:
 - if `container Concurrency is 0`, the load balancing will be random to any pod available.
 - if `container Concurrency is 3 or less`, the load balancing will use the first available pod.
 - if `container Concurrency is more than 3`, the load balancing will use round robin between available pods.

Unlike the queue-proxy, the activator actively sends metrics to the autoscaler via a websocket connection to minimize scale-from-zero latencies as much as possible.

## Autoscaler
The Autoscaler is responsible for collecting metrics from the queue-proxies of the different application instances and comes up with the desired the number of pods an application needs using the basic calculation:
```
want = concurrencyInSystem/targetConcurrencyPerInstance
```
About the calculations:
 - Requests stats are stored in a ring buffer indexed by timeToIndex() % len(elements in ring). Each element represents a certain granularity of time, and the total represented duration adds up to a window length of time.
 - With each new request, a value is added with an associated time to the correct element. If this record would introduce a gap in the data, any intervening times between the last write and this one will be recorded as zero. If an entire window length has expired without data, the firstWrite time is reset, meaning the WindowAverage will be of a partial window until enough data is received to fill it again.
 - For getting the average over a window of time, the average element value over the window is calculated. If the first write was less than the window length ago, an average is returned over the partial window. For example, if firstWrite was 6 seconds ago, the average will be over these 6 seconds worth of elements, even if the window is 60s. If a window passes with no data being received, the first write time is reset so this behavior takes effect again.
 - Similarly, if we have not received recent data, the average is based on a partial window. For example, if the window is 60 seconds but we last received data 10 seconds ago, the window average will be the average over the first 50 seconds. In other cases, for example if there are gaps in the data shorter than the window length, the missing data is assumed to be 0 and the average is over the whole window length inclusive of the missing data.
 - If too many requests appear in the short time, the Autoscaler panics, which means it decides the required scale based on a shorter window. In normal scenarios, the Autoscaler decides on a trailing average of the past 60 seconds, but in panic mode it's on the last 6 seconds only. This makes the decisions more sensitive to bursty traffic.

In addition to that, the autoscaler adjusts the value for a maximum scale up/down rate and the min- and max-instances settings on the revision. It also computes how much burst capacity is left in the current deployment and thus determines whether or not the activator can be taken off of the data-path or not.

Details about the API and data flow when scaling up/down are [here](SYSTEM.md)
