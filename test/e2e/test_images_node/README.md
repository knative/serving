# Test images node

The subdirectories found in this directory contains the `Dockerfiles` for the
test images used in the e2e tests.

## Building

You can build these images with[`upload-test-images.sh`]
(/test/README.md#e2e-test-images).

## Adding new images

New test images should be placed in their own subdirectories. Be sure to add any
new subdirectories to [`upload-test-images.sh`](/test/README.md#e2e-test-images)
and to include a `Dockerfile` for building and running the test image.

For instance, if I am adding a new subdirectory, `my_new_image`, which contains
`my_new_image.go` and `Dockerfile`, I would need to change the version loop in
[`upload-test-images.sh`](/test/README.md#e2e-test-images) from

```shell
for version in autoscale helloworld
```

to

```shell
for version in autoscale helloworld my_new_image
```

Additionally, the new test images will also need to be uploaded to the e2e tests
Docker repo. You will need one of the owners found in
[`/test/e2e/OWNERS`](../OWNERS) to do this.
