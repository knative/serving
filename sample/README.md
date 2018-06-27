# Samples

This directory contains sample services which demonstrate `Knative Serving`
functionality.

## Prerequisites

[Install Knative Serving](https://github.com/knative/install/blob/master/README.md)

## Samples

* [helloworld](./helloworld) - A simple webserver written in Go
* [pythonsimple](./pythonsimple) - A simple webserver written in Python
* [service](./service) - A simple Go example using the Service resource
* [stock restful app](./stock-rest-app) - Simple Restful service
* [thumbnailer](./thumbnailer) - A 'dockerized' web application creating thumbnails from videos
* [steren's sample-app](./steren-app) - A simple Node.js web application
* [buildpack sample app](./buildpack-app) - A sample buildpack app
* [buildpack sample function](./buildpack-function) - A sample buildpack function
* [steren's sample-function](./steren-function) - A simple Node.js function
* [private repos](./private-repos/) - A sample illustrating private GitHub / DockerHub
* [telemetrysample](./telemetrysample) - A simple webserver emitting logs and metrics
* [gitwebhook](./gitwebhook) - A function that listens for git PR changes and updates the title of them
* [autoscaler](./autoscale) - A demonstration of revision autoscaling
* [knative routing](./knative-routing) - A demonstration of mapping multiple Knative services 
under a single domain.

## Best Practices for Contributing to Samples
* Minimize dependencies on third party libraries and prefer using standard libraries. Examples:
    * Use "log" for logging.
