# Resource Types

The primary resources in the Elafros API are Routes, Revisions, and Configurations:

* A **Route** provides a named endpoint and a mechanism for routing traffic to

* **Revisions**, which are immutable snapshots of code + config, created by a

* **Configuration**, which acts as a stream of environments for Revisions.

![Object model](images/object_model.png)

## Route

**Route** provides a network endpoint for a user's service (which
consists of a series of software and configuration Revisions over
time). A kubernetes namespace can have multiple routes. The route
provides a long-lived, stable, named, HTTP-addressable endpoint that
is backed by one or more **Revisions**. The default configuration is
for the route to automatically route traffic to the latest revision
created by a **Configuration**. For more complex scenarios, the API
supports splitting traffic on a percentage basis, and CI tools could
maintain multiple configurations for a single route (e.g. "golden
path" and “experiments”) or reference multiple revisions directly to
pin revisions during an incremental rollout and n-way traffic
split. The route can optionally assign addressable subdomains to any
or all backing revisions.

## Revision

**Revision** is an immutable snapshot of code and configuration. A
revision can be created from a pre-built container image or built from
source. While there is a history of previous revisions, only those
currently referenced by a Route are addressable or routable. Older
inactive revisions need not be backed by underlying resources, they
exist only as the revision metadata in storage. Revisions are created
by updates to a **Configuration**.

## Configuration

A **Configuration** describes the desired latest Revision state, and
creates and tracks the status of Revisions as the desired state is
updated. A configuration might include instructions on how to transform
a source package (either git repo or archive) into a container by
referencing a [Build](https://github.com/elafros/build), or might
simply reference a container image and associated execution metadata
needed by the Revision. On updates to a Configuration, a new build
and/or deployment (creating a Revision) may be performed; the
Configuration's controller will track the status of created Revisions
and makes both the most recently created and most recently *ready*
(i.e. healthy) Revision available in the status section.


# Orchestration

The system will be configured to not allow customer mutations to
Revisions. Instead, the creation of immutable Revisions through a
Configuration provides:

* a single referenceable resource for the route to perform automated
  rollouts
* a single resource that can be watched to see a history of all the
  revisions created
* (but doesn’t mandate) PATCH semantics for new revisions to be done
  on the server, minimizing read-modify-write implemented across
  multiple clients, which could result in optimistic concurrency
  errors
* the ability to rollback to a known good configuration

In the conventional single live revision scenario, a route has a
single configuration with the same name as the route. Update
operations on the configuration enable scenarios such as:

* *"Push code, keep config":* Specifying a new revision with updated
  source, inheriting configuration such as env vars from the
  configuration.
* *"Update config, keep code"*: Specifying a new revision as just a
  change to configuration, such as updating an env variable,
  inheriting all other configuration and source/image.

When creating an initial route and performing the first deployment,
the two operations of creating a Route and an associated Configuration
can be done in parallel, which streamlines the use case of deploying
code initially from a button. The
[sample API usage](normative_examples.md) section illustrates
conventional usage of the API.
