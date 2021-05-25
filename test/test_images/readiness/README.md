# Ready test image

This directory contains the test image used to simulate an image that responds
to various types of readiness probe.

The image provides a /healthz endpoint which will reply with a 200 status code
and the Hello World text only after the delay requested by the STARTUP_DELAY
environment variable has elapsed.

The image also contains an exec probe when run with "probe" as its only argument.

## Trying out

To run the image as a Service outside of the test suite:

`ko apply -f service.yaml`

## Building

For details about building and adding new images, see the
[section about test images](/test/README.md#test-images).
