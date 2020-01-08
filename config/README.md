# Welcome to the knative/serving config directory!

The files in this directory are organized as follows:

- `core/`: the elements that are required for knative/serving to function,
- `istio-ingress/`: the configuration needed to plug in the istio ingress
  implementation,
- `hpa-autoscaling/`: the configuration needed to extend the core with HPA-class
  autoscaling,
- `namespace-wildcards/`: the configuration needed to extend the core to
  provision wildcard certificates per-namespace,
- `cert-manager/`: the configuration needed to plug in the `cert-manager`
  certificate implementation,
- `monitoring/`: an installable bundle of tooling for assorted observability
  functions,
- `*.yaml`: symlinks that form a particular "rendered view" of the
  knative/serving configuration.

## Core

The Core is complex enough that it further breaks down as follows:

- `rbac/`: The service accounts, [cluster] roles, and [cluster] role bindings
  needed for the core controllers to function, or to plug knative/serving into
  standard Kubernetes RBAC constructs.
- `configmaps/`: The configmaps that are used to configure the core components.
- `resources/`: The serving resource definitions.
- `webhooks/`: The serving {mutating, validating} admission webhook
  configurations, and supporting resources.
- `deployments/`: The serving executable components and associated configuration
  resources.
