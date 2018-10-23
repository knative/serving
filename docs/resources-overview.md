# Resources Overview

For a truly high-level overview, [see these slides](https://docs.google.com/presentation/d/1CbwVC7W2JaSxRyltU8CS1bIsrIXu1RrZqvnlMlDaaJE/edit#slide=id.p)

This document provides a high-level description of the resources deployed to a Kubernetes cluster in order to run Knative Serving. The exact list of resources is going to change frequently during the current phase of active development. In order to keep this document from becoming out-of-date frequently it doesn't describe the exact individual resources but instead the higher level objects which they form.

## Dependencies

Knative Serving depends on two other projects in order to function: [Istio][istio] and the [Build CRD][build-crd]. Istio is responsible for setting up the network routing both inside the cluster and ingress into the cluster. The Build CRD provides a custom resource for Kubernetes which provides an extensible primitive for creating container images from various sources, for example a Git repository.

You can find out more about both from their respective websites.

[istio]: https://istio.io/
[build-crd]: https://github.com/knative/build

## Components

There are two primary components to the Knative Serving system. The first is a controller which is responsible for updating the state of the cluster based on user input. The second is the webhook component which handles validation of the objects and actions performed.

The controller processes a series of state changes in order to move the system from its current, actual state to the state desired by the user.

All of the Knative Serving components are deployed into the `knative-serving` namespace. You can see the various objects in this namespace by running `kubectl -n knative-serving get all` ([minus some admin-level resources like service accounts](https://github.com/kubernetes/kubectl/issues/151)). To see only objects of a specific type, for example to see the webhook and controller deployments inside Knative Serving, you can run `kubectl -n knative-serving get deployments`.

The Knative Serving controller creates Kubernetes, Istio, and Build CRD resources when Knative Serving resources are created and updated. These sub-resources will be created in the same namespace as their parent Knative Serving resource, _not_ the `knative-serving` namespace.

## Kubernetes Resource Configs

The various Kubernetes resource configurations are organized as follows:

```plain
# Knative Serving resources
config/*.yaml

# Knative Serving Monitoring configs
config/monitoring/...

# Build resources
third_party/config/build/release.yaml

# Istio release configuration
third_party/istio-0.6.0/install/kubernetes/...
```

## Viewing resources after deploying Knative Serving

### Custom Resource Definitions

To view all of the custom resource definitions created, run `kubectl get customresourcedefinitions`. These resources are named according to their group, i.e. custom resources required by Knative Serving end with `knative.dev`.

### Deployments

View the Knative Serving specific deployments by running `kubectl -n knative-serving get deployments`. These deployments will ensure that the correct number of pods are running for that specific deployment.

For example, given:

```console
$ kubectl -n knative-serving get deployments
NAME             DESIRED   CURRENT   UP-TO-DATE   AVAILABLE   AGE
controller   1         1         1            1           6m
webhook      1         1         1            1           6m
```

Based on the desired state shown above, we expect there to be a single pod running for each of the deployments shown above. We can verify this by running and seeing similar output as shown below:

```console
$ kubectl -n knative-serving get pods
NAME                              READY     STATUS    RESTARTS   AGE
controller-5bfb798f96-2zjnf   1/1       Running   0          9m
webhook-64c459569b-v5npx      1/1       Running   0          8m
```

Similarly, you can run the same commands in the build-crd (`knative-build`) and istio (`istio-system`) namespaces to view the running deployments. To view all namespaces, run `kubectl get namespaces`.

### Service Accounts and RBAC policies

To view the service accounts configured for Knative Serving, run `kubectl -n knative-serving get serviceaccounts`.

To view all cluster role bindings, run `kubectl get clusterrolebindings`. Unfortunately there is currently no mechanism to fetch the cluster role bindings that are tied to a service account.
