# Prow Test Job Image

This directory contains the custom Docker image used by our Prow test jobs.

## Building and publishing a new image

To build and push a new image, just run `make push`.

For testing purposes you can build an image but not push it; to do so, run `make build`.

Note that you must have proper permission in the `knative-tests` project to push new images to the GCR.

The `prow-tests` image is pinned on a specific `kubekins` image; update `Dockerfile` if you need to use a newer/different image. This will basically define the versions of `bazel`, `go`, `kubectl` and other build tools.
