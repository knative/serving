# Logs and metrics

First, deploy monitoring components. You can use two different setups:
1. **everything**: This configuration collects logs & metrics from user containers, build controller and istio requests.
```shell
# With kubectl
kubectl apply -R -f config/monitoring/100-common \
    -f config/monitoring/150-prod \
    -f third_party/config/monitoring \
    -f config/monitoring/200-common \
    -f config/monitoring/200-common/100-istio.yaml

# With bazel
bazel run config/monitoring:everything.apply
```

2. **everything-dev**: This configuration collects everything in (1) plus Elafros controller logs.
```shell
# With kubectl
kubectl apply -R -f config/monitoring/100-common \
    -f config/monitoring/150-dev \
    -f third_party/config/monitoring \
    -f config/monitoring/200-common \
    -f config/monitoring/200-common/100-istio.yaml

# With bazel
bazel run config/monitoring:everything-dev.apply
```

## Accessing logs
Run,

```shell
kubectl proxy
```
Then open Kibana UI at this [link](http://localhost:8001/api/v1/namespaces/monitoring/services/kibana-logging/proxy/app/kibana)
(*it might take a couple of minutes for the proxy to work*).
When Kibana is opened the first time, it will ask you to create an index. Accept the default options as is. As logs get ingested,
new fields will be discovered and to have them indexed, go to Management -> Index Patterns -> Refresh button (on top right) -> Refresh fields.

## Accessing metrics

Run:

```shell
kubectl port-forward -n monitoring $(kubectl get pods -n monitoring --selector=app=grafana --output=jsonpath="{.items..metadata.name}") 3000
```

Then open Grafana UI at [http://localhost:3000](http://localhost:3000). The following dashboards are pre-installed with Elafros:
* **Revision HTTP Requests:** HTTP request count, latency and size metrics per revision and per configuration
* **Nodes:** CPU, memory, network and disk metrics at node level
* **Pods:** CPU, memory and network metrics at pod level
* **Deployment:** CPU, memory and network metrics aggregated at deployment level 
* **Istio, Mixer and Pilot:** Detailed Istio mesh, Mixer and Pilot metrics
* **Kubernetes:** Dashboards giving insights into cluster health, deployments and capacity usage

## Accessing per request traces
First open Kibana UI as shown above. Browse to Management -> Index Patterns -> +Create Index Pattern and type "zipkin*" (without the quotes) to the "Index pattern" text field and hit "Create" button. This will create a new index pattern that will store per request traces captured by Zipkin. This is a one time step and is needed only for fresh installations.

Next, start the proxy if it is not already running:

```shell
kubectl proxy
```

Then open Zipkin UI at this [link](http://localhost:8001/api/v1/namespaces/istio-system/services/zipkin:9411/proxy/zipkin/). Click on "Find Traces" to see the latest traces. You can search for a trace ID or look at traces of a specific application within this UI. Click on a trace to see a detailed view of a specific call.

To see a demo of distributed tracing, deploy the [Telemetry sample](../sample/telemetrysample/README.md), send some traffic to it and explore the traces it generates from Zipkin UI.

## Default metrics
Following metrics are collected by default:
* Elafros controller metrics
* Istio metrics (mixer, envoy and pilot)
* Node and pod metrics

There are several other collectors that are pre-configured but not enabled. To see the full list, browse to config/monitoring/prometheus-exporter and config/monitoring/prometheus-servicemonitor folders and deploy them using kubectl apply -f.

## Default logs
Deployment above enables collection of the following logs:
* stdout & stderr from all ela-container
* stdout & stderr from build-controller

To enable log collection from other containers and destinations, edit fluentd-es-configmap.yaml (search for "fluentd-containers.log" for the starting point). Then run the following:
```shell
kubectl replace -f config/monitoring/fluentd/fluentd-es-configmap.yaml
kubectl replace -f config/monitoring/fluentd/fluentd-es-ds.yaml
```

Note: We will enable a plugin mechanism to define other logs to collect and this step is a workaround until then.

## Metrics troubleshooting
You can use Prometheus web UI to troubleshoot publishing and service discovery issues for metrics.
To access to the web UI, forward the Prometheus server to your machine:

```shell
kubectl port-forward -n monitoring $(kubectl get pods -n monitoring --selector=app=prometheus --output=jsonpath="{.items[0].metadata.name}") 9090
```

Then browse to http://localhost:9090 to access the UI:
* To see the targets that are being scraped, go to Status -> Targets
* To see what Prometheus service discovery is picking up vs. dropping, go to Status -> Service Discovery

## Generating metrics

See [Telemetry Sample](../sample/telemetrysample/README.md) for deploying a dedicated instance of Prometheus
and emitting metrics to it.

If you want to generate metrics within Elafros services and send them to shared instance of Prometheus,
follow the steps below. We will create a counter metric below:
1. Go through [Prometheus Documentation](https://prometheus.io/docs/introduction/overview/)
and read [Data model](https://prometheus.io/docs/concepts/data_model/) and
[Metric types](https://prometheus.io/docs/concepts/metric_types/) sections.
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
  namespace: monitoring
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
definitions is to use Grafana UI (make sure to login with as admin user) and [export JSON](http://docs.grafana.org/reference/export_import) from it.

8. Redeploy changes.

9. Validate the metrics flow either by Grafana UI or Prometheus UI (see Troubleshooting section
above to enable Prometheus UI)

## Generating logs
Use [glog](https://godoc.org/github.com/golang/glog) to write logs in your code. In your container spec, add the following args to redirect the logs to stderr:
```yaml
args:
- "-logtostderr=true"
- "-stderrthreshold=INFO"
```

See [helloworld](../sample/helloworld/README.md) sample's configuration file as an example.

## Distributed tracing with Zipkin
Check [Telemetry sample](../sample/telemetrysample/README.md) as an example usage of [OpenZipkin](https://zipkin.io/pages/existing_instrumentations)'s Go client library.
