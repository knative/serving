# Elafros Developer Logging and Monitoring Contract

This document is intended to set an agreement with Elafros
[developer persona](../product/personas.md#developer-personas) about the logging
and monitoring environment where their applications, container images and functions.
run.

## Logging

Elafros provides default out of the box logs for all of applications,
container images and functions.

### Log Types

The following logs types are supported.

* **Request logs**: Status of requests or invocations sent to the applications, containers
  or functions.
* **stdout/stderr**: Logs emitted by the applications, containers or functions
  to the stdout/stderr channels.
* **/var/log**: All files under `/var/log` will be collected and parsed line by line.
  If a single line message is a JSON payload, it will be treated as structured logs and parsed accordingly.
  See the [Logs Formats](#log-formats) section for more information.
  **NOTE**: Collecting `/var/log` requires a sidecar container.
  [Operators](../product/personas.md#operator-personas) can enable/disable this feature.
* **/dev/log**: TBD.

Elafros recommends sending logs to stdout/stderr.

### Log Destinations

Log aggregation is explicitly pluggable. [Operators](../product/personas.md#operator-personas)
can choose logging plugins to define the log destinations.

Elafros provides samples of logging plugins, e.g. ElasticSearch&Kibana, Stackdriver.
Cluster operator can implement their own plugins.

### Log Formats

The following formats are supported.

* **Plain text**: A single line regarded as plain text. The following metadata
  will be extracted from the log record:
  * *kubernetes.labels.elafros_dev/configuration*: The name of Elafros configuration
    of the application, container or function that emitted the log.
  * *kubernetes.labels.elafros_dev/revision*: The name of Elafros revision of the application,
     container or function that emitted the log.
  * *kubernetes.namespace_name*: The name of Kubernetes namespace of the application, container
    or function that emitted the log.
  * *log*: The original log content.
  * *stream*: One of `stdout`, `stderr` or `varlog`.
  * *tag*: If the log was from stdout/stderr, the value is
    `kubernetes.var.log.<pod_name>_<namespace>_<container_name>_<docker_id>.log`.
    If the log was from `/var/log/*`, the value is the relative path to `/var/log/`
    with "`/`" replaced with "`.`". For example, the value of a log from
    `/var/log/foo/bar.log` is `foo.bar.log`.
  * *time*: Time when the log was collected. **NOTE**: Developers need to add
    timestamp in the log content if they want the timestamp to be accurate.

* **Structured**: A single line of serialized JSON. If a single line fails to be
  parsed into JSON, it will be treated as plain text. For example, if a log is
  emitted as `{"message": "Hello"}`, the following metadata will be extracted
  from the log record:

  * *message*: Lifted from JSON dictionary. `Hello` in this case.
  * *kubernetes.labels.elafros_dev/configuration*, *kubernetes.labels.elafros_dev/revision*,
    *kubernetes.namespace_name*, *stream*, *tag*, *time*: Same with plain text.

* **Multi-line**: If a consecutive sequence of log messages forms an exception stack
  trace, the log messages are forwarded as a single, combined log message.

* **Request logs**: Request logs are structured as follows:

  * *destinationConfiguration*: Elafros Configuration that served the request.
  * *destinationNamespace*: Namespace that the request was served on.
  * *destinationRevision*: Elafros Revision that served the request.
  * *latency*: Time took for the request to complete.
  * *method*: HTTP request method (GET, POST, etc).
  * *protocol*: http, https or tcp.
  * *referer*: Address of the previous web page from which the request was made.
  * *requestHost*: Domain name of the service processing the request.
  * *requestSize*: Size of the request.
  * *responseCode*: HTTP response code.
  * *responseSize*: Size of the response.
  * *tag*: A fixed value set to “requestlog.logentry.istio-system” - used to identify request logs from other logs.
  * *timestamp*: Time request was made.
  * *traceId*: OpenTracing trace id.
  * *url*: Relative URL that was requested.
  * *userAgent*: User agent string of the request.

  **NOTE**: There is no extra payload/message field.

### Log Cleanup

Logs written to stdout and stderr are cleaned up by Kubernetes. Elafros will
provide a [DaemonSet](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/)
per cluster to rotate all the logs from `/var/log/*`.

## Monitoring

### Default Metrics

#### revision_request_count

Description: Number of times an application, a container or a function has been called
since it was deployed.

Type: Counter

Labels:

* *destination_configuration*: Elafros Configuration that served the request.
* *destination_namespace*: Kubernetes namespace that the request was served on.
* *destination_revision*: Elafros Revision that served the request.
* *response_code*: HTTP response code.
* *source_service*: If the request was from outside the cluster, this will be
  istio-ingress service name. If the request was from inside the cluster (revisions
  calling other revisions), this will be Kubernetes service name of the revision.

#### revision_request_duration

Description: Time it took for an application, a container or a function to handle request.

Type: Histogram

Labels:

* *destination_configuration*: Elafros Configuration that served the request.
* *destination_namespace*: Kubernetes namespace that the request was served on.
* *destination_revision*: Elafros Revision that served the request.
* *response_code*: HTTP response code.
* *source_service*: If the request was from outside the cluster, this will be
  istio-ingress service name. If the request was from inside the cluster (revisions
  calling other revisions), this will be Kubernetes service name of the revision.

#### revision_request_size

Description: Size of requests to an application, a container or a function.

Type: Histogram

Labels:

* *destination_configuration*: Elafros Configuration that served the request.
* *destination_namespace*: Kubernetes namespace that the request was served on.
* *destination_revision*: Elafros Revision that served the request.
* *response_code*: HTTP response code.
* *source_service*: If the request was from outside the cluster, this will be
  istio-ingress service name. If the request was from inside the cluster (revisions
  calling other revisions), this will be Kubernetes service name of the revision.

#### revision_response_size

Description: Size of response from an application, a container or a function.

Type: Histogram

Labels:

* *destination_configuration*: Elafros Configuration that served the request.
* *destination_namespace*: Kubernetes namespace that the request was served on.
* *destination_revision*: Elafros Revision that served the request.
* *response_code*: HTTP response code.
* *source_service*: If the request was from outside the cluster, this will be
  istio-ingress service name. If the request was from inside the cluster (revisions
  calling other revisions), this will be Kubernetes service name of the revision.

#### container_memory_usage_bytes

Description: Memory usage of a container.

Type: Gauge

Labels:

* *container_name*: Name of the container.
* *namespace*: Kubernetes namespace that the container was served on.
* *pod_name*: Name of Kubernetes pod that the container was served on.

#### container_cpu_usage_seconds_total

Description: CPU usage of a container.

Type: Counter

Labels:

* *container_name*: Name of the container.
* *cpu*: CPU identification, cpu00, cpu01, etc.
* *namespace*: Kubernetes namespace that the container was served on.
* *pod_name*: Name of Kubernetes pod that the container was served on.

### Metrics Destinations

The default destination of all metrics will be Prometheus. Operators can setup
exporters on Prometheus to send the metrics to other destinations. Grafana will
be used as the dashboard tool to visualize these metrics.

### Custom Metrics

Custom metrics are not supported in this release but are considered for future
releases. For users who want to generate custom metrics, we strongly recommend
using [OpenCensus](https://opencensus.io/) libraries as we plan to integrate
with OpenCensus in order to support custom metrics.

## Distributed Tracing

Request traces are automatically generated on behalf of the developer by Istio.
Initial release will support Zipkin, Jaeger and Stackdriver as destinations. We
are also working with Istio team to support OpenCensus instrumentation library
and take advantage of a single client library that works with a rich set of backend
services. Once Istio adds OpenCensus support, our plan is to extend the list of
supported backends via OpenCensus.

Support for custom spans is still TBD. We will provide custom span support with
Zipkin and Jaeger backends; however custom spans with Stackdriver backend is not
supported by Istio yet.

Lastly, we want to allow customizing the sampling policy for distributed traces
as well as request logs but sampling is not supported in the current Istio release.
We are in active discussions with Istio team to add sampling support.

## Samples

We will provide samples written in Java, Python, PHP, Go, Ruby, C# and Node.js
to showcase the usage of logging, metrics and distributed tracing features that
come with Elafros.
