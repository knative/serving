# Autoscale test image

This directory contains the test image used in the autoscale e2e tests.

The image contains a simple Go webserver, `autoscale.go`, that will listen on
port `8080` and expose a service at `/`.

The service applies different modes of resource consumption based on query
parameters.

- `sleep=X` -- number of milliseconds to sleep (e.g.
  `curl http://${URL}/?sleep=200`), alternatively duration can be specified as
  `time.Duration`, e.g. `sleep=13s`.
- `sleep-stddev=X` -- valid only if `sleep` is provided, sleeps for a random
  period of time according to a normal distribution centered around `sleep` with
  stddev equal to this value.
- `bloat=X` -- creates a byte array size of `X*1024*1024` and assigns 1 to each
  array value (to ensure heap allocation).
- `prime=X` -- computes the smallest prime less than `X`. Does not support
  `X > 40000000`.

## Trying out

To run the image as a Service outisde of the test suite:

`ko apply -f service.yaml`

## Building

For details about building and adding new images, see the
[section about test images](/test/README.md#test-images).
