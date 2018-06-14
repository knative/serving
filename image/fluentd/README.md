# Fluentd Docker Image on Knative Serving

Knative Serving uses a [Fluentd](https://www.fluentd.org/) docker image to collect
logs. Operators can customize their own docker image and configuration to
define logging output.

## Requirements

Knative requires the following Fluentd plugins to process log records:

* [fluentd](https://github.com/fluent/fluentd) >= v0.14.0
* [fluent-plugin-kubernetes_metadata_filter](https://github.com/fabric8io/fluent-plugin-kubernetes_metadata_filter) >= 1.0.0 AND < 2.1.0
* [fluent-plugin-detect-exceptions](https://github.com/GoogleCloudPlatform/fluent-plugin-detect-exceptions) >= 0.0.9
* [fluent-plugin-multi-format-parser](https://github.com/repeatedly/fluent-plugin-multi-format-parser) >= 1.0.0

## Sample images

Operators can use any Docker image which meets the requirements
above and includes the desired output plugin. Two examples below:

### Send logs to Elasticsearch

Operators can use
[k8s.gcr.io/fluentd-elasticsearch:v2.0.4](https://github.com/kubernetes/kubernetes/tree/master/cluster/addons/fluentd-elasticsearch/fluentd-es-image)
which includes
[fluent-plugin-elasticsearch](https://github.com/uken/fluent-plugin-elasticsearch)
that allows sending logs to a Elasticsearch service.

### Send logs to Stackdriver

This sample [Dockerfile](stackdriver/Dockerfile) is based on `k8s.gcr.io/fluentd-elasticsearch:v2.0.4`.
It additionally adds one more plugin -
[fluent-plugin-google-cloud](https://github.com/GoogleCloudPlatform/fluent-plugin-google-cloud)
which allows sending logs to Stackdriver.

Operators can build this image and push it to a container registry which
their Kubernetes cluster has access to. **NOTE**: Operators need to add
credentials file the stackdriver agent needs to the docker image if their
Knative Serving is not built on a GCP based cluster or they want to send logs to
another GCP project. See [here](https://cloud.google.com/logging/docs/agent/authorization) for more information.
