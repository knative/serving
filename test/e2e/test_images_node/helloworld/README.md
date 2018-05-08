# Helloworld test image

This directory contains the `Dockerfile` used for the test image used in the helloworld e2e tests.

The image contains a simple Go webserver, `helloworld.go`, that will, by default, listen on port `8080` and expose a service at `/`.

When called, the server emits a "hello world" message.

## Building
You can build this image with [`upload-test-images.sh`](/test/README.md#e2e-test-images).
