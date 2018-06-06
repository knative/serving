# Monitoring Deployment

This folder contains deployment files for monitoring components. For example, if
operators require sending standard logs to **Elasticsearch**, they can install
monitoring components by running the following at the root of the repository:

```shell
kubectl apply -R -f config/monitoring/100-common \
    -f config/monitoring/150-elasticsearch-prod \
    -f third_party/config/monitoring/common \
    -f third_party/config/monitoring/elasticsearch \
    -f config/monitoring/200-common \
    -f config/monitoring/200-common/100-istio.yaml
```

See [Logs and metrics](doc/telemetry.md) for setting up other logging and
monitoring backends.

`kubectl -R -f` installs the files within a folder in alphabetical order.
In order to install the files with correct ordering within a folder,
a three digit prefix is added.

* Files with a prefix require files with smaller prefixes to be installed before they are installed.
* Files with the same prefix can be installed in any order within the set sharing the same prefix.
* Files without any prefix can be installed in any order and they don't have any dependencies.
* The root folder (`config/monitoring`) is special. It requires the following installation ordering:

  * `/config/monitoring/100-common`
  * Only one of `/config/monitoring/150-*`. File with `dev` postfix is a special
    configuration that enables verbose logging and should only be used for development
    purposes. File with `elasticsearch` or `stackdriver` indicates the logging destination.
  * `/third_party/config/monitoring/common`
  * `/third_party/config/monitoring/elasticsearch`. Required only when Elasticsearch is used as logging destination.
  * `/config/monitoring/200-common`
  * `100-istio.yaml` is installed a second time due to a bug in Istio 0.6.0, which requires metric
    and logging changes to be applied a second time to work.
