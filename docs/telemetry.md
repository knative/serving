# Logs, metrics and traces

Install monitoring components using
[Monitoring, Logging and Tracing Installation](https://github.com/knative/docs/blob/master/serving/installing-logging-metrics-traces.md).
Once finished, visit
[Knative Serving](https://github.com/knative/docs/tree/master/serving)
for guides on accessing logs, metrics and traces.

## Default metrics

The following metrics are collected by default:

* Knative Serving controller metrics
* Istio metrics (mixer, envoy and pilot)
* Node and pod metrics

There are several other collectors that are pre-configured but not enabled.
To see the full list, browse to config/monitoring/prometheus-exporter
and config/monitoring/prometheus-servicemonitor folders and deploy them
using `kubectl apply -f`.

## Default logs

Deployment above enables collection of the following logs:

* stdout & stderr from all user-container
* stdout & stderr from build-controller

To enable log collection from other containers and destinations, see
[setting up a logging plugin](https://github.com/knative/docs/blob/master/serving/setting-up-a-logging-plugin.md).

## Metrics troubleshooting

You can use the Prometheus web UI to troubleshoot publishing and service
discovery issues for metrics. To access to the web UI, forward the Prometheus
server to your machine:

```shell
kubectl port-forward -n knative-monitoring $(kubectl get pods -n knative-monitoring --selector=app=prometheus --output=jsonpath="{.items[0].metadata.name}") 9090
```

Then browse to http://localhost:9090 to access the UI.

* To see the targets that are being scraped, go to Status -> Targets
* To see what Prometheus service discovery is picking up vs. dropping, go to Status -> Service Discovery

## Generating metrics

If you want to send metrics from your controller, follow the steps below. These
steps are already applied to autoscaler and controller. For those controllers,
simply add your new metric definitions to the `view`, create new `tag.Key`s if
necessary and instrument your code as described in step 3.

In the example below, we will setup the service to host the metrics and
instrument a sample 'Gauge' type metric using the setup.

1. First, go through [OpenCensus Go Documentation](https://godoc.org/go.opencensus.io).
2. Add the following to your application startup:

```go
import (
	"net/http"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	desiredPodCountM *stats.Int64Measure
	namespaceTagKey  tag.Key
	revisionTagKey   tag.Key
)

func main() {
	exporter, err := prometheus.NewExporter(prometheus.Options{Namespace: "{your metrics namespace (eg: autoscaler)}"})
	if err != nil {
		glog.Fatal(err)
	}
	view.RegisterExporter(exporter)
	view.SetReportingPeriod(10 * time.Second)

	// Create a sample gauge
	var r = &Reporter{}
	desiredPodCountM = stats.Int64(
		"desired_pod_count",
		"Number of pods autoscaler wants to allocate",
		stats.UnitNone)

	// Tag the statistics with namespace and revision labels
	var err error
	namespaceTagKey, err = tag.NewKey("namespace")
	if err != nil {
		// Error handling
	}
	revisionTagKey, err = tag.NewKey("revision")
	if err != nil {
		// Error handling
	}

	// Create view to see our measurement.
	err = view.Register(
		&view.View{
			Description: "Number of pods autoscaler wants to allocate",
			Measure:     r.measurements[DesiredPodCountM],
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{namespaceTagKey, configTagKey, revisionTagKey},
		},
	)
	if err != nil {
		// Error handling
	}

	// Start the endpoint for Prometheus scraping
	mux := http.NewServeMux()
	mux.Handle("/metrics", exporter)
	http.ListenAndServe(":8080", mux)
}
```

3.In your code where you want to instrument, set the counter with the
appropriate label values - example:

```go
ctx := context.TODO()
tag.New(
    ctx,
    tag.Insert(namespaceTagKey, namespace),
    tag.Insert(revisionTagKey, revision))
stats.Record(ctx, desiredPodCountM.M({Measurement Value}))
```

4.Add the following to scape config file located at
config/monitoring/200-common/300-prometheus/100-scrape-config.yaml:

```yaml
- job_name: <YOUR SERVICE NAME>
    kubernetes_sd_configs:
    - role: endpoints
    relabel_configs:
    # Scrape only the the targets matching the following metadata
    - source_labels: [__meta_kubernetes_namespace, __meta_kubernetes_service_label_app, __meta_kubernetes_endpoint_port_name]
    action: keep
    regex: {SERVICE NAMESPACE};{APP LABEL};{PORT NAME}
    # Rename metadata labels to be reader friendly
    - source_labels: [__meta_kubernetes_namespace]
    action: replace
    regex: (.*)
    target_label: namespace
    replacement: $1
    - source_labels: [__meta_kubernetes_pod_name]
    action: replace
    regex: (.*)
    target_label: pod
    replacement: $1
    - source_labels: [__meta_kubernetes_service_name]
    action: replace
    regex: (.*)
    target_label: service
    replacement: $1
```

5.Redeploy prometheus and its configuration:

```shell
kubectl delete -f config/monitoring/200-common/300-prometheus
kubectl apply -f config/monitoring/200-common/300-prometheus
```

6.Add a dashboard for your metrics - you can see examples of it under
config/grafana/dashboard-definition folder. An easy way to generate JSON
definitions is to use Grafana UI (make sure to login with as admin user) and
[export JSON](http://docs.grafana.org/reference/export_import) from it.

7.Validate the metrics flow either by Grafana UI or Prometheus UI (see
Troubleshooting section above to enable Prometheus UI)

## Distributed tracing with Zipkin

Check [Telemetry sample](https://github.com/knative/docs/tree/master/serving/samples/telemetry-go)
as an example usage of [OpenZipkin](https://zipkin.io/pages/existing_instrumentations)'s Go client library.

## Delete monitoring components

Enter:

```shell
ko delete --ignore-not-found=true -f config/monitoring/100-namespace.yaml
```
