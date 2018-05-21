# Monitoring Deployment

This folder contains deployment files for monitoring components.
These can be installed by running the following at the root of the repository:

```shell
kubectl apply -R -f config/monitoring/100-common \
    -f config/monitoring/150-prod \
    -f third_party/config/monitoring \
    -f config/monitoring/200-common \
    -f config/monitoring/200-common/100-istio.yaml
```

`kubectl -R -f` installs the files within a folder in alphabetical order.
In order to install the files with correct ordering within a folder,
a three digit prefix is added.

* Files with a prefix require files with smaller prefixes to be installed before they are installed.
* Files with the same prefix can be installed in any order within the set sharing the same prefix.
* Files without any prefix can be installed in any order and they don't have any dependencies.
* The root folder (`config/monitoring`) is special. It requires the following installation ordering:

    * `/config/monitoring/100-common`
    *  Either `/config/monitoring/150-prod` or `/config/monitoring/150-dev`, but not both. `150-dev` is a
       special configuration that enables verbose logging and should only be used for development purposes.
    * `/third_party/config/monitoring`
    * `/config/monitoring/200-common`
    * `100-istio.yaml` is installed a second time due to a bug in istio 0.6.0, which requires metric
     and logging changes to be applied a second time to work.
