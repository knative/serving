# Contributor Samples

This directory contains samples useful for Knative Serving contributors.

Samples for Knative end users (developers and operators) are available in the
[Knative Docs repo](https://github.com/knative/docs/tree/master/serving/samples).

## Prerequisites

[Install Knative Serving](https://github.com/knative/install/blob/master/README.md)

## Samples

* [buildpack sample app](./buildpack-app) - A sample buildpack app
* [buildpack sample function](./buildpack-function) - A sample buildpack function

## Best Practices for Contributing to Samples

* Minimize dependencies on third party libraries and prefer using standard 
  libraries. Examples:
  * Use "log" for logging.
