# Assorted scripts for development

This directory contains several scripts useful in the development process of Knative Serving.

* `boilerplate/add-boilerplate.sh` Adds license boilerplate to *txt* or *go* files in a directory, recursively.
* `deploy.sh` Deploys Knative Serving to an [environment](environments.md).
* `diagnose-me.sh` Performs several diagnostic checks on the running Kubernetes cluster, for debugging.
* `release.sh` Creates a new [release](release.md) of Knative Serving.
* `update-codegen.sh` Updates auto-generated client libraries.
* `update-deps.sh` Updates Go dependencies.
* `verify-codegen.sh` Verifies that auto-generated client libraries are up-to-date.
