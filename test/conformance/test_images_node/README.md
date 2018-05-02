# Test images node

This directory contains the `Dockerfiles` for two test images used
in the conformance tests.

The images contain a node.js webserver that
will by default listens on port `8080` and expose a service at `/`.

The two versions of the image ([`Dockerfile.v1`](./Dockerfile.v1),
[`Dockerfile.v2`](./Dockerfile.v2)) differ in the message they return
when you hit `/`.

## Building

You can build these images with [`upload-test-images.sh`](/test/README.md#conformance-test-images).