# Logs and metrics

## Monitoring components Setup

First, deploy monitoring components.

### Elasticsearch, Kibana, Prometheus & Grafana Setup

You can use two different setups:

1. **150-elasticsearch-prod**: This configuration collects logs & metrics from user containers, build controller and Istio requests.

```shell
kubectl apply -R -f config/monitoring/100-common \
    -f config/monitoring/150-elasticsearch-prod \
    -f third_party/config/monitoring/common \
    -f third_party/config/monitoring/elasticsearch \
    -f config/monitoring/200-common \
    -f config/monitoring/200-common/100-istio.yaml
```

1. **150-elasticsearch-dev**: This configuration collects everything in (1) plus Knative Serving controller logs.

```shell
kubectl apply -R -f config/monitoring/100-common \
    -f config/monitoring/150-elasticsearch-dev \
    -f third_party/config/monitoring/common \
    -f third_party/config/monitoring/elasticsearch \
    -f config/monitoring/200-common \
    -f config/monitoring/200-common/100-istio.yaml
```

### Stackdriver(logs), Prometheus & Grafana Setup

If your Knative Serving is not built on a GCP based cluster or you want to send logs to
another GCP project, you need to build your own Fluentd image and modify the
configuration first. See

1. [Fluentd image on Knative Serving](/image/fluentd/README.md)
2. [Setting up a logging plugin](setting-up-a-logging-plugin.md)

Then you can use two different setups:

1. **150-stackdriver-prod**: This configuration collects logs & metrics from user containers, build controller and Istio requests.

```shell
kubectl apply -R -f config/monitoring/100-common \
    -f config/monitoring/150-stackdriver-prod \
    -f third_party/config/monitoring/common \
    -f config/monitoring/200-common \
    -f config/monitoring/200-common/100-istio.yaml
```

2. **150-stackdriver-dev**: This configuration collects everything in (1) plus Knative Serving controller logs.

```shell
kubectl apply -R -f config/monitoring/100-common \
    -f config/monitoring/150-stackdriver-dev \
    -f third_party/config/monitoring/common \
    -f config/monitoring/200-common \
    -f config/monitoring/200-common/100-istio.yaml
```

## Accessing logs

### Elasticsearch & Kibana

Run,

```shell
kubectl proxy
```

Then open Kibana UI at this [link](http://localhost:8001/api/v1/namespaces/monitoring/services/kibana-logging/proxy/app/kibana)
(*it might take a couple of minutes for the proxy to work*).
When Kibana is opened the first time, it will ask you to create an index. Accept the default options as is. As more logs get ingested,
new fields will be discovered and to have them indexed, go to Management -> Index Patterns -> Refresh button (on top right) -> Refresh fields.

#### Accessing configuration and revision logs

To access to logs for a configuration, use the following search term in Kibana UI:
```
kubernetes.labels.knative_dev\/configuration: "configuration-example"
```

Replace `configuration-example` with your configuration's name.

To access logs for a revision, use the following search term in Kibana UI:

```
kubernetes.labels.knative_dev\/revision: "configuration-example-00001"
```

Replace `configuration-example-00001` with your revision's name.

#### Accessing build logs

To access to logs for a build, use the following search term in Kibana UI:

```
kubernetes.labels.build\-name: "test-build"
```

Replace `test-build` with your build's name. A build's name is specified in its YAML file as follows:

```yaml
apiVersion: build.dev/v1alpha1
kind: Build
metadata:
  name: test-build
```

### Stackdriver

Go to [Pantheon logging page](https://console.cloud.google.com/logs/viewer) for
your GCP project which stores your logs via Stackdriver.

## Accessing metrics

Run:

```shell
kubectl port-forward -n monitoring $(kubectl get pods -n monitoring --selector=app=grafana --output=jsonpath="{.items..metadata.name}") 3000
```

Then open Grafana UI at [http://localhost:3000](http://localhost:3000). The following dashboards are pre-installed with Knative Serving:

* **Revision HTTP Requests:** HTTP request count, latency and size metrics per revision and per configuration
* **Nodes:** CPU, memory, network and disk metrics at node level
* **Pods:** CPU, memory and network metrics at pod level
* **Deployment:** CPU, memory and network metrics aggregated at deployment level
* **Istio, Mixer and Pilot:** Detailed Istio mesh, Mixer and Pilot metrics
* **Kubernetes:** Dashboards giving insights into cluster health, deployments and capacity usage

### Accessing per request traces

First open Kibana UI as shown above. Browse to Management -> Index Patterns -> +Create Index Pattern and type "zipkin*" (without the quotes) to the "Index pattern" text field and hit "Create" button. This will create a new index pattern that will store per request traces captured by Zipkin. This is a one time step and is needed only for fresh installations.

Next, start the proxy if it is not already running:

```shell
kubectl proxy
```

Then open Zipkin UI at this [link](http://localhost:8001/api/v1/namespaces/istio-system/services/zipkin:9411/proxy/zipkin/). Click on "Find Traces" to see the latest traces. You can search for a trace ID or look at traces of a specific application within this UI. Click on a trace to see a detailed view of a specific call.

To see a demo of distributed tracing, deploy the [Telemetry sample](../sample/telemetrysample/README.md), send some traffic to it and explore the traces it generates from Zipkin UI.

## Default metrics

Following metrics are collected by default:
* Knative Serving controller metrics
* Istio metrics (mixer, envoy and pilot)
* Node and pod metrics

There are several other collectors that are pre-configured but not enabled. To see the full list, browse to config/monitoring/prometheus-exporter and config/monitoring/prometheus-servicemonitor folders and deploy them using kubectl apply -f.

## Default logs

Deployment above enables collection of the following logs:

* stdout & stderr from all user-container
* stdout & stderr from build-controller

To enable log collection from other containers and destinations, see
[setting up a logging plugin](setting-up-a-logging-plugin.md).

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

If you want to send metrics from your controller, follow the steps below.
These steps are already applied to autoscaler and controller. For those controllers,
simply add your new metric definitions to the `view`, create new `tag.Key`s if necessary and
instrument your code as described in step 3.

In the example below, we will setup the service to host the metrics and instrument a sample
'Gauge' type metric using the setup.

1. First, go through [OpenCensus Go Documentation](https://godoc.org/go.opencensus.io).
2. Add the following to your application startup:

```go
import (
	"context"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
    desiredPodCountM *stats.Int64Measure
    namespaceTagKey tag.Key
    revisionTagKey tag.Key
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
3. In your code where you want to instrument, set the counter with the appropriate label values - example:

```go
ctx := context.TODO()
tag.New(
    ctx,
    tag.Insert(namespaceTagKey, namespace),
    tag.Insert(revisionTagKey, revision))
stats.Record(ctx, desiredPodCountM.M({Measurement Value}))
```

4. Add the following to scape config file located at config/monitoring/200-common/300-prometheus/100-scrape-config.yaml:

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

5. Redeploy prometheus and its configuration:
```sh
kubectl delete -f config/monitoring/200-common/300-prometheus
kubectl apply -f config/monitoring/200-common/300-prometheus
```

6. Add a dashboard for your metrics - you can see examples of it under
config/grafana/dashboard-definition folder. An easy way to generate JSON
definitions is to use Grafana UI (make sure to login with as admin user) and [export JSON](http://docs.grafana.org/reference/export_import) from it.

7. Validate the metrics flow either by Grafana UI or Prometheus UI (see Troubleshooting section
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
