# Resource Types

The primary resources in the Knative Serving API are Routes, Revisions,
Configurations, and Services:

- A **Route** provides a named endpoint and a mechanism for routing traffic to

- **Revisions**, which are immutable snapshots of code + config, created by a

- **Configuration**, which acts as a stream of environments for Revisions.

- **Service** acts as a top-level container for managing the set of Routes and
  Configurations which implement a network service.

![Object model](images/object_model.png)

## Route

**Route** provides a network endpoint for a user's service (which consists of a
series of software and configuration Revisions over time). A kubernetes
namespace can have multiple routes. The route provides a long-lived, stable,
named, HTTP-addressable endpoint that is backed by one or more **Revisions**.
The default configuration is for the route to automatically route traffic to the
latest revision created by a **Configuration**. For more complex scenarios, the
API supports splitting traffic on a percentage basis, and CI tools could
maintain multiple configurations for a single route (e.g. "golden path" and
“experiments”) or reference multiple revisions directly to pin revisions during
an incremental rollout and n-way traffic split. The route can optionally assign
addressable subdomains to any or all backing revisions.

## Revision

**Revision** is an immutable snapshot of code and configuration. A revision
references a container image. Revisions are created by updates to a
**Configuration**.

Revisions that are not addressable via a Route may be garbage collected and all
underlying K8s resources will be deleted. Revisions that are addressable via a
Route will have resource utilization proportional to the load they are under.

## Configuration

A **Configuration** describes the desired latest Revision state, and creates and
tracks the status of Revisions as the desired state is updated. A configuration
will reference a container image and associated execution metadata needed by the
Revision. On updates to a Configuration's spec, a new Revision will be created;
the Configuration's controller will track the status of created Revisions and
makes the most recently created and most recently _ready_ Revisions available in
the status section.

## Service

A **Service** encapsulates a **Route** and **Configuration** which together
provide a software component. Service exists to provide a singular abstraction
which can be access controlled, reasoned about, and which encapsulates software
lifecycle decisions such as rollout policy and team resource ownership. Service
acts only as an orchestrator of the underlying Routes and Configurations (much
as a kubernetes Deployment orchestrates ReplicaSets), and its usage is optional
but recommended.

The Service's controller will track the statuses of its owned Configuration and
Route, reflecting their statuses and conditions as its own.

The owned Configuration's Ready conditions are surfaced as the Service's
ConfigurationsReady condition. The owned Routes' Ready conditions are surfaced
as the Service's RoutesReady condition.

## Orchestration

The system will be configured to disallow users from creating
([NYI](https://github.com/knative/serving/issues/664)) or changing Revisions.
Instead, Revisions are created indirectly when a Configuration is created or
updated. This provides:

- a single referenceable resource for the route to perform automated rollouts
- a single resource that can be watched to see a history of all the revisions
  created
- PATCH semantics for revisions implemented server-side, minimizing
  read-modify-write implemented across multiple clients, which could result in
  optimistic concurrency errors
- the ability to rollback to a known good configuration

In the conventional single live revision scenario, a service creates both a
route and a configuration with the same name as the service. Update operations
on the service enable scenarios such as:

- _"Push image, keep config":_ Specifying a new revision with updated image,
  inheriting configuration such as env vars from the configuration.
- _"Update config, keep image"_: Specifying a new revision as just a change to
  configuration, such as updating an env variable, inheriting all other
  configuration and image.
- _"Execute a controlled rollout"_: Updating the service's traffic spec allows
  testing of revisions before making them live, and controlled rollouts.

The [sample API usage](normative_examples.md) section illustrates conventional
usage of the API.
