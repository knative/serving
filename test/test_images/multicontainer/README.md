# MultiContainer test

This directory contains the images used in the multicontainer e2e test.

There are three images servingcontainer, sidecarcontainerone and
sidecarcontainertwo.

All three image contains a simple Go webserver those are listening on port
`8881`, `8882`, `8883` respectively.

Out of three one will be the serving(serves the request) container which expose
a service at `/` and other two are sidecar containers

When called, the serving container emits a "Yay!! multi-container works" message
by talking to remaining sidecar containers

## Trying out

To run the image as a Service outisde of the test suite:

`ko apply -f service.yaml`

## Building

For details about building and adding new images, see the
[section about test images](/test/README.md#test-images).
