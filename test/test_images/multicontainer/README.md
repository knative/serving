# MultiContainer test

This directory contains the images used in the multicontainer e2e test.

There are two images servingcontainer and sidecarcontainer.

Both image contains a simple Go webserver and listens on port `8881`, `8882`
respectively.

Out of which one will be the serving (serves the request) container which
exposes a service at `/` and the other will be the sidecar container.

When called, the serving container emits a "Yay!! multi-container works" message
by talking to sidecar container.

## Trying out

To run the image as a Service outside of the test suite:

`ko apply -f service.yaml`

## Building

For details about building and adding new images, see the
[section about test images](/test/README.md#test-images).
