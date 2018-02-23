# Collecting and generating telemetry

Deploy Prometheus, service monitors and Grafana:
```shell
bazel run config/prometheus:everything.create
bazel run config/grafana:everything.create
bazel run config/grafana:everything-public.create
```

To see pre-installed system dashboards, find the public IP that Grafana is exposed on:

```shell
# Put the Ingress IP into an environment variable.
$ kubectl get service grafana-public -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"
```

And browse to <IP_ADDRESS>:30802.

**Above installs Grafana with a hard coded admin username (_admin_) and password (_admin_) 
and publishes it on a public IP. This should only be done in a development environment with no sensitive data.**

## Troubleshooting

You can expose Prometheus UI to troubleshoot metric publishing or service discovery issues:

```shell
bazel run config/prometheus:everything-public.create
```

**Run this only in a development environment with no sensitive data. This step enables viewing all metrics on a public IP without any authorization.**

Wait until the load balancer IP is created and then copy the IP address:

```shell
# Put the Ingress IP into an environment variable.
$ kubectl get service prometheus-system-public -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"
```

And browse to <IP_ADDRESS>:30800.

## Generating metrics

See [Telemetry Sample](../sample/telemetrysample/README.md) for deploying a dedicated instance of Prometheus 
and emitting metrics to it.

If you want to generate metrics within Elafros services and send them to shared instance of Prometheus, 
follow the steps below. We will create a counter metric below:
1. Go through [Prometheus Documentation](https://prometheus.io/docs/introduction/overview/) 
and read [Data model](https://prometheus.io/docs/concepts/data_model/) and [Metric types](https://prometheus.io/docs/concepts/metric_types/) sections.
2. Create a top level variable in your go file and initialize it in init() - example:

```go
    import "github.com/prometheus/client_golang/prometheus"
    
    var myCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
        Namespace: "elafros",
        Name:      "mycomponent_something_count",
        Help:      "Counter to keep track of something in my component",
    }, []string{"status"})
    
    func init() {
        prometheus.MustRegister(myCounter)
    }
```
3. In your code where you want to instrument, increment the counter with the appropriate label values - example:

```go
    err := doSomething()
    if err == nil {
        myCounter.With(prometheus.Labels{"status": "success"}).Inc()
    } else {
        myCounter.With(prometheus.Labels{"status": "failure"}).Inc()
    }
```
4. Start an http listener to serve as the metrics endpoint for Prometheus scraping (_this step and onwards are needed 
only once in a service. ela-controller is already setup for metrics scraping and you can skip rest of these steps
if you are targeting ela-controller_):

```go
    import "github.com/prometheus/client_golang/prometheus/promhttp"
    
    // In your main() func
    srv := &http.Server{Addr: ":9090"}
    http.Handle("/metrics", promhttp.Handler())
    go func() {
        if err := srv.ListenAndServe(); err != nil {
            glog.Infof("Httpserver: ListenAndServe() finished with error: %s", err)
        }
    }()

    // Wait for the service to shutdown
    <-stopCh

    // Close the http server gracefully
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    srv.Shutdown(ctx)

```

5. Add a Service for the metrics http endpoint:

```yaml
apiVersion: v1
kind: Service
metadata:
  labels:
    app: myappname
    prometheus: myappname
  name: myappname
  namespace: mynamespace
spec:
  ports:
  - name: metrics
    port: 9090
    protocol: TCP
    targetPort: 9090
  selector:
    app: myappname # put the appropriate value here to select your application
```

6. Add a ServiceMonitor to tell Prometheus to discover pods and scrape the service defined above:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: myappname
  labels:
    monitor-category: ela-system # Shared Prometheus instance only targets 'k8s', 'istio', 'node',
                                 # 'prometheus' or 'ela-system' - if you pick something else, 
                                 # you need to deploy your own Prometheus instance or edit shared
                                 # instance to target the new category
spec:
  selector:
    matchLabels:
      app: myappname
      prometheus: myappname
  namespaceSelector:
    matchNames:
    - mynamespace
  endpoints:
  - port: metrics
    interval: 30s
```

7. Add a dashboard for your metrics - you can see examples of it under 
config/grafana/dashboard-definition folder. An easy way to generate JSON 
definitions is to use Grafana UI (make sure to login with as admin user) and export JSON from it.

8. Add the YAML files to BUILD files.

9. Deploy changes with bazel.

10. Validate the metrics flow either by Grafana UI or Prometheus UI (see Troubleshooting section 
above to enable Prometheus UI)