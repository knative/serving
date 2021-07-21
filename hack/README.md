# Assorted scripts for development

This directory contains several scripts useful in the development process of
Knative Serving.

- `boilerplate/add-boilerplate.sh` Adds license boilerplate to _txt_ or _go_
  files in a directory, recursively.
- `generate-yamls.sh` Builds all the YAMLs that Knative Serving publishes. To run it locally, run `export YAML_LIST=$(mktemp) && export REPO_ROOT_DIR=$(pwd) && ./hack/generate-yamls.sh "${REPO_ROOT_DIR}" "${YAML_LIST}"`
- `release.sh` Creates a new release of Knative Serving.
- `update-codegen.sh` Updates auto-generated client libraries.
- `update-checksums.sh` Updates the `knative.dev/example-checksum` annotations
  in config maps.
- `update-deps.sh` Updates Go dependencies.
- `verify-codegen.sh` Verifies that auto-generated client libraries are
  up-to-date.
