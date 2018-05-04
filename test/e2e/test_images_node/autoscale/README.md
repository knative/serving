# Autoscale test image

This directory contains the `Dockerfile` used for the test image used in the autoscale e2e tests.

The image contains a simple Go webserver, `autoscale.go` that will, by default, listen on port `8080` and expose a service at `/`.

When called, the server calculates the highest prime less than 40000000 and emits a message.

## Building
You can build this image with [`upload-test-images.sh`](/test/README.md#e2e-test-images).
