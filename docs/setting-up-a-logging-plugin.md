# Setting Up A Logging Plugin

Elafros allows operators to configure the logging output, i.e. log
record format, logging destination, etc. This
instruction is based on current state of Elafros. An
[effort](https://github.com/elafros/elafros/issues/906)
is in process to abstract logging configuration.

## Configuring

### Configure the DaemonSet for stdout/stderr logs

Operators can do the following steps to configure the Fluentd DaemonSet for
collecting `stdout/stderr` logs from the containers:

1. Configure the DaemonSet ouput rules via `900.output.conf` part in
   [fluentd-configmap.yaml](/config/monitoring/fluentd-configmap.yaml).
   Elafros provides samples for sending logs to Elasticsearch or Stackdriver.
   Developers can simply choose one of `150-*` from
   [/config/monitoring](/config/monitoring) or override any with other
   configuration.
1. Configure the sidecar image via `image` field of `fluentd-ds` container
   in [fluentd-ds.yaml](/third_party/config/monitoring/common/fluentd/fluentd-ds.yaml).
   See [here](/image/fluentd/README.md) for the requirements for Flunetd image.

### Configure the Sidecar for log files under /var/log

Currently operators have to configure the Fluentd Sidecar separately for
collecting log files under `/var/log`. An
[effort](https://github.com/elafros/elafros/issues/818)
is in process to get rid of the sidecar. The steps to configure are:

1. Configure the sidecar output rules via `logging.fluentd-sidecar-output-config`
   flag in [elaconfig](/config/elaconfig.yaml). In theory, this is the same
   with the one for Fluentd DaemonSet.
2. Configure the sidecar image via `logging.fluentd-sidecar-image` flag in
   [elaconfig](/config/elaconfig.yaml). In theory, this is the same
   with the one for Fluentd DaemonSet.

## Deploying

Operators need to deploy Elafros components after the configuring.

### Deploy the DaemonSet

If `config/monitoring/150-stackdriver-prod` is the desired output rules,
the logging component can be deployed by:

```shell
kubectl apply -f config/monitoring/150-stackdriver-prod/fluentd-confimap.yaml \
    -f third_party/config/monitoring/common/kubernetes/fluentd/fluentd-ds.yaml \
    -f config/monitoring/200-common/100-fluentd.yaml
    -f config/monitoring/200-common/100-istio.yaml
```

**NOTE**: Operators need to deploy the services they desire to satisfy their
logging output. For example, if they desire Elasticsearch&Kibana as the logging
storage and viewer, they have to deploy the Elasticsearch and Kibana services.
Elafros provides this sampel:

```shell
kubectl apply -R -f third_party/config/monitoring/elasticsearch
```

See [here](/config/monitoring/README.md) for deploying the whole Elafros
monitoring components.

### Deploy the Sidecar

This requires deploying Elafros controller, which can be done by:

```shell
kubectl apply -f config/elaconfig.yaml -f config/controller.yaml
```

**NOTE**: Same with DaemonSet, operators also need to deploy the services they
desire to satisfy their logging output.
