# Ready test image

This directory contains the test image used to simulate an image that responds
to various types of readiness probe.

The image provides an endpoint which will reply with a 200 status code
and the Hello World text only after the delay requested by the READY_DELAY
environment variable has elapsed.

The image also contains an exec probe when run with "probe" as its only argument.

## Controlled startup health

Set `STARTUP_BLOCKED=true` to start the HTTP listener with failing health checks.
Send `POST /start-succeeding` to the main server to release health. Repeated POSTs
are safe; other methods on this endpoint return 405 in blocked mode. The endpoint
is registered only in blocked mode. Existing behavior is unchanged when the
setting is absent. Do not combine this setting with `READY_DELAY`.

Each process restart resets blocked health. This test-only mode lets a harness
observe application startup blocking before releasing a native sidecar's startup
probe. Probe success is an initial startup gate, not ongoing helper-health
monitoring. Use only synthetic test configuration in an isolated test cluster.

## Trying out

To run the image as a Service outside of the test suite:

`ko apply -f service.yaml`

## Building

For details about building and adding new images, see the
[section about test images](/test/README.md#test-images).
