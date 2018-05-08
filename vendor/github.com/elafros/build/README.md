# Build CRD

This repository implements `Build` and `BuildTemplate` custom resources
for Kubernetes, and a controller for making them work.

If you are interested in contributing, see [CONTRIBUTING.md](./CONTRIBUTING.md)
and [DEVELOPMENT.md](./DEVELOPMENT.md).

## Objective

Kubernetes is emerging as the predominant (if not de facto) container
orchestration layer.  It is also quickly becoming the foundational layer on top
of which the ecosystem is building higher-level compute abstractions (PaaS,
FaaS).  In order to increase developer velocity, these higher-level compute
abstractions typically operate on source, not just containers, which must be
built.  However, there is no common building block today for bridging these
worlds.

This repository provides an implementation of the Build [CRD](
https://kubernetes.io/docs/concepts/api-extension/custom-resources/) that runs
Builds on-cluster (by default), because that is the lowest common denominator
that we expect users to have available.  It is also possible to write
[pkg/builder](./pkg/builder) that delegates Builds to hosted services (e.g.
Google Container Builder), but these are typically more restrictive.

The aim of this project isn't to be a complete standalone product that folks use
directly (e.g. as a CI/CD replacement), but a building block to facilitate the
expression of Builds as part of larger systems.

## Getting Started

You can install the latest release of the Build CRD by running:
```shell
kubectl create -f https://storage.googleapis.com/build-crd/latest/release.yaml
```

## Terminology and Conventions

* [Builds](./builds.md)
* [Build Templates](./build-templates.md)
* [Builders](./builder-contract.md)
* [Authentication](./cmd/creds-init/README.md)
