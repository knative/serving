# Setting Up A Logging Plugin

Knative allows cluster operators to use different backends for their logging
needs. This document describes how to change these settings. Knative currently
requires changes in Fluentd configuration files, however we plan on abstracting
logging configuration in the future
([#906](https://github.com/knative/serving/issues/906)). Once
[#906](https://github.com/knative/serving/issues/906) is complete, the
methodology described in this document will no longer be valid and migration to
a new process will be required. In order to minimize the effort for a future
migration, we recommend only changing the output configuration of Fluentd and
leaving the rest intact.

## Configuring

### Configure the DaemonSet for stdout/stderr logs

Operators can do the following steps to configure the Fluentd DaemonSet for
collecting `stdout/stderr` logs from the containers:

1. Replace `900.output.conf` part in
   [fluentd-configmap.yaml](/config/monitoring/fluentd-configmap.yaml) with the
   desired output configuration. Knative provides samples for sending logs to
   Elasticsearch or Stackdriver. Developers can simply choose one of `150-*`
   from [/config/monitoring](/config/monitoring) or override any with other
   configuration.
1. Replace the `image` field of `fluentd-ds` container
   in [fluentd-ds.yaml](/third_party/config/monitoring/common/fluentd/fluentd-ds.yaml)
   with the Fluentd image including the desired Fluentd output plugin.
   See [here](/image/fluentd/README.md) for the requirements of Flunetd image
   on Knative.

### Configure the Sidecar for log files under /var/log

Currently operators have to configure the Fluentd Sidecar separately for
collecting log files under `/var/log`. An
[effort](https://github.com/knative/serving/issues/818)
is in process to get rid of the sidecar. The steps to configure are:

1. Replace `logging.fluentd-sidecar-output-config` flag in
   [config-observability](/config/config-observability.yaml)  with the
   desired output configuration. **NOTE**: The Fluentd DaemonSet is in
   `monitoring` namespace while the Fluentd sidecar is in the namespace same with
   the app. There may be small differences between the configuration for DaemonSet
   and sidecar even though the desired backends are the same.
1. Replace `logging.fluentd-sidecar-image` flag in
   [config-observability](/config/config-observability.yaml) with the Fluentd image including the
   desired Fluentd output plugin. In theory, this is the same
   with the one for Fluentd DaemonSet.

## Deploying

Operators need to deploy Knative components after the configuring:

```shell
# In case there is no change with the controller code
bazel run config:controller.delete
# Deploy the configuration for sidecar
kubectl apply -f config/config-observability.yaml
# Deploy the controller to make configuration for sidecar take effect
bazel run config:controller.apply

# Deploy the DaemonSet to make configuration for DaemonSet take effect
kubectl apply -f <the-fluentd-config-for-daemonset> \
    -f third_party/config/monitoring/common/kubernetes/fluentd/fluentd-ds.yaml \
    -f config/monitoring/200-common/100-fluentd.yaml
    -f config/monitoring/200-common/100-istio.yaml
```

In the commands above, replace `<the-fluentd-config-for-daemonset>` with the
Fluentd DaemonSet configuration file, e.g. `config/monitoring/150-stackdriver-prod`.

**NOTE**: Operators sometimes need to deploy extra services as the logging
backends. For example, if they desire Elasticsearch&Kibana, they have to deploy
the Elasticsearch and Kibana services. Knative provides this sample:

```shell
kubectl apply -R -f third_party/config/monitoring/elasticsearch
```

See [here](/config/monitoring/README.md) for deploying the whole Knative
monitoring components.
