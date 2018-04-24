# Elafros Developer Logging and Monitoring Contract

This document is intended to set an agreement with Elafros
[developer persona](../product/personas.md#developer-personas) about the logging
and monitoring environment where their applications, container images and functions.
run.

## Logging

Elafros provides default out of the box logs and dashboards for all of applications,
container images and functions.

### Log Types

The following logs are collected.

* **Request logs**: Status of requests or invocations sent to the applications, containers
  or functions. Collected automatically by default.
* **stdout/stderr**: Logs emitted by the applications, containers or functions
  to the stdout/stderr channels. Collected automatically by default.
* **/var/log**: All files under `/var/log` will be collected and parsed as single line.
  If the message is a JSON payload, it will be treated as structured logs and parsed accordingly.
  See [Logs Format](#log-formats) section for more information. **NOTE**:
  [Operators](../product/personas.md#operator-personas) can enable/disable this feature.

Elafros recommends to send logs to stdout/stderr.

### Log Destinations

The default destination of all logs is a in-cluster instance of ElasticSearch. A
Kibana dashboard is provided as the default UI to view logs.

Stackdriver is provided as an alternate logging destination.

### Log Formats

The following formats are supported.

* **Plain text**: A single line regarded as plain text, structured as follows:
  * *log*: The original log content.
  * *tag*: If the log was from stdout/stderr, the value is
    `kubernetes.var.log.<pod_name>_<namespace>_<container_name>_<container_id>.log`.
    If the log was from `/var/log/*`, the value is the relative path to `/var/log/`
    with "`/`" replaced with "`.`". For example, the value of a log from
    `/var/log/foo/bar.log` is `foo.bar.log`.
  * *time*: Time when the log was collected.
  * *kubernetes.namespace_name*: Kubernetes namespace of the application, container
    or function that emitted the log.
  * *kubernetes.labels.elafros_dev/configuration*: Elafros configuration of the
    application, container or function that emitted the log.
  * *kubernetes.labels.elafros_dev/revision*: Elafros revision of the application,
     container or function that emitted the log.
  * *stream*: One of `stdout`, `stderr` or `varlog`.

  **NOTE**:

* **Structured**: A single line of serialized JSON. For example, if a log was
  emitted as `{"message": "Hello", "fluentd-time": "2018-05-23T12:42:22.14423454"}`,
  it will be structured as follows:

  * *time*: Lifted from `fluentd-time` in JSON dictionary. **NOTE**: the format
    should be `%Y-%m-%dT%H:%M:%S.%NZ` otherwise the log will not be parsed correctly.
    If this key is missing, the value will be the time when the log was collected.
  * *message*: Lifted from JSON dictionary.
  * *tag*, *kubernetes.namespace_name*, *kubernetes.labels.elafros_dev/configuration*
    *kubernetes.labels.elafros_dev/revision*, *stream*: Same with plant text.

* **Multi-line**: If a consecutive sequence of log messages forms an exception stack
  trace, the log messages are forwarded as a single, combined log message.

* **Request logs**: Request logs are structured as follows:

  * *tag*: A fixed value set to “requestlog.logentry.istio-system” - used to identify
  request logs from other logs.
  * *timestamp*: Time request was made.
  * *destinationNamespace*: Namespace that the request was served on.
  * *destinationConfiguration*: Elafros Configuration that served the request.
  * *destinationRevision*: Elafros Revision that served the request.
  * *requestHost*: Domain name of the service processing the request.
  * *requestSize*: Size of the request.
  * *responseCode*: HTTP response code.
  * *responseSize*: Size of the response.
  * *latency*: Time took for the request to complete.
  * *method*: HTTP request method (GET, POST, etc).
  * *protocol*: http, https or tcp.
  * *url*: Relative URL that was requested.
  * *traceId*: OpenTracing trace id

### Log Cleanup

Logs written to stdout and stderr are cleaned up by Kubernetes. Elafros will
provide necessary functionality to clean up all the logs from `/var/log/*` that
are collected.

## Monitoring

### Default Metrics

#### revision_request_count

Description: Number of times an application, a container or a function has been called.

Type: Counter

Labels:

* *source_service*: Service that the request was from.
* *destination_namespace*: Kubernetes namespace that the request was served on.
* *destination_configuration*: Elafros Configuration that served the request.
* *destination_revision*: Elafros Revision that served the request.
* *response_code*: HTTP response code.

#### revision_request_duration

Description: Time it took for an application, a container or a function to handle request.

Type: Histogram

Labels:

* *source_service*: Service that the request was from.
* *destination_namespace*: Kubernetes namespace that the request was served on.
* *destination_configuration*: Elafros Configuration that served the request.
* *destination_revision*: Elafros Revision that served the request.
* *response_code*: HTTP response code.

#### revision_request_size

Description: Size of requests to an application, a container or a function.

Type: Histogram

Labels:

* *source_service*: Service that the request was from.
* *destination_namespace*: Kubernetes namespace that the request was served on.
* *destination_configuration*: Elafros Configuration that served the request.
* *destination_revision*: Elafros Revision that served the request.
* *response_code*: HTTP response code.

#### revision_response_size

Description: Size of response to an application, a container or a function.

Type: Histogram

Labels:

* *source_service*: Service that the request was from.
* *destination_namespace*: Kubernetes namespace that the request was served on.
* *destination_configuration*: Elafros Configuration that served the request.
* *destination_revision*: Elafros Revision that served the request.
* *response_code*: HTTP response code.

#### container_memory_usage_bytes

Description: Memory usage of a container.

Type: Gauge

Labels:

* *container_name*: Name of the container.
* *pod_name*: Name of Kubernetes pod that the container was served on.
* *namespace*: Kubernetes namespace that the container was served on.

#### container_cpu_usage_seconds_total

Description: CPU usage of a container.

Type: Gauge

Labels:

* *container_name*: Name of the container.
* *pod_name*: Name of Kubernetes pod that the container was served on.
* *namespace*: Kubernetes namespace that the container was served on.
* *cpu*: CPU identification, cpu00, cpu01, etc.

### Metrics Destinations

The default destination of all metrics will be Prometheus. Operators can setup
exporters on Prometheus to send the metrics to other destinations. Grafana will
be used as the dashboard tool to visualize these metrics.

### Custom Metrics

Custom metrics are not supported in this release but are considered for future
releases. For users who want to generate custom metrics, we strongly recommend
using [OpenCensus](https://opencensus.io/ ) libraries as we plan to integrate
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
as well as request logs but sampling is not support in the current Istio release.
We are in active discussions with Istio team to add sampling support.

## Samples

We will provide samples written in Java, Python, PHP, Go, Ruby, C# and Node.js
to showcase the usage of logging, metrics and distributed tracing features that
come with Elafros.
