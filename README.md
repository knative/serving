# Elafros

This repository contains an open source specification and implementation of a Kubernetes- and Istio-based container platform.

If you are interested in contributing to `Elafros`, see
[CONTRIBUTING.md](./CONTRIBUTING.md) and [DEVELOPMENT.md](./DEVELOPMENT.md).

* [Setup your development environment](./DEVELOPMENT.md#getting-started)
* [Starting Elafros](#start-elafros)
* [Run samples](./sample/README.md)

## Start Elafros

Once you've [setup your development
environment](./DEVELOPMENT.md#getting-started), stand up `Elafros` with:

```shell
bazel run :everything.create
```

This will:
 * Build the `ela-controller` into a Docker container.
 * Publish the `ela-controller` container to `{DOCKER_REPO_OVERRIDE}/ela-controller:latest`.
 * Create a number of resources, including:
   * A `Namespace` in which we run Elafros components.
   * A `ServiceAccount` as which Elafros will authorize requests.
   * A `ClusterRoleBinding`, which grants the Elafros service account the capability to interact with
   cluster resources.
   * The `CustomResourceDefinition`s for Elafros resources.
   * The `Deployment` running the Elafros controller.

You can see things running with:
```shell
$ kubectl -n ela-system get pods
NAME                                READY     STATUS    RESTARTS   AGE
ela-controller-77897cc687-vp27q   1/1       Running   0          16s
```

You can access the Elafros Controller's logs with:

```shell
$ kubectl -n ela-system logs $(kubectl -n ela-system get pods -l app=ela-controller -o name)
```

## Clean up

You can delete all of the service components with:
```shell
bazel run :elafros.delete
bazel run :istio.delete
```

Due to [a bazel deletion ordering issue](https://github.com/bazelbuild/rules_k8s/issues/97),
which also prevents `bazel run :everything.delete` from working,
both of the above commands will output errors such as:

```
deployments.extensions "ela-webhook" not found
...
serviceaccounts "istio-mixer-service-account" not found
```

Deleting the Custom Resource Definitions will cascade and cause any instances
of those resources to be cleaned up.
