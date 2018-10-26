# Helper scripts

This directory contains helper scripts used by Prow test jobs, as well and
local development scripts.

## Using the `e2e-tests.sh` helper script

This is a helper script for Knative E2E test scripts. To use it:

1. Source the script.

1. [optional] Write the `teardown()` function, which will tear down your test
resources.

1. [optional] Write the `dump_extra_cluster_state()` function. It will be
called when a test fails, and can dump extra information about the current state
of the cluster (tipically using `kubectl`).

1. Call the `initialize()` function passing `$@` (without quotes).

1. Write logic for the end-to-end tests. Run all go tests using `go_test_e2e()`
(or `report_go_test()` if you need a more fine-grained control) and call
`fail_test()` or `success()` if any of them failed. The environment variables
`DOCKER_REPO_OVERRIDE`, `K8S_CLUSTER_OVERRIDE` and `K8S_USER_OVERRIDE` will be set
according to the test cluster. You can also use the following boolean (0 is false,
1 is true) environment variables for the logic:
    * `EMIT_METRICS`: true if `--emit-metrics` was passed.
    * `USING_EXISTING_CLUSTER`: true if the test cluster is an already existing one,
and not a temporary cluster created by `kubetest`.

    All environment variables above are marked read-only.

**Notes:**

1. Calling your script without arguments will create a new cluster in the GCP
project `$PROJECT_ID` and run the tests against it.

1. Calling your script with `--run-tests` and the variables `K8S_CLUSTER_OVERRIDE`,
`K8S_USER_OVERRIDE` and `DOCKER_REPO_OVERRIDE` set will immediately start the
tests against the cluster.

### Sample end-to-end test script

This script will test that the latest Knative Serving nightly release works.

```bash
source vendor/github.com/knative/test-infra/scripts/e2e-tests.sh

function teardown() {
  echo "TODO: tear down test resources"
}

initialize $@

start_latest_knative_serving

wait_until_pods_running knative-serving || fail_test "Knative Serving is not up"

# TODO: use go_test_e2e to run the tests.
kubectl get pods || fail_test

success
```

## Using the `release.sh` helper script

This is a helper script for Knative release scripts. To use it:

1. Source the script.

1. Call the `initialize()` function passing `$@` (without quotes).

1. Call the `run_validation_tests()` function passing the script or executable that
runs the release validation tests. It will call the script to run the tests unless
`--skip_tests` was passed.

1. Write logic for the release process. Call `publish_yaml()` to publish the manifest(s),
`tag_releases_in_yaml()` to tag the generated images, `branch_release()` to branch
named releases. Use the following boolean (0 is false, 1 is true) and string environment
variables for the logic:
    * `RELEASE_VERSION`: contains the release version if `--version` was passed. This
also overrides the value of the `TAG` variable as `v<version>`.
    * `RELEASE_BRANCH`: contains the release branch if `--branch` was passed. Otherwise
it's empty and `master` HEAD will be considered the release branch.
    * `RELEASE_NOTES`: contains the filename with the release notes if `--release-notes`
was passed. The release notes is a simple markdown file.
    * `SKIP_TESTS`: true if `--skip-tests` was passed. This is handled automatically
by the run_validation_tests() function.
    * `TAG_RELEASE`: true if `--tag-release` was passed. In this case, the environment
variable `TAG` will contain the release tag in the form `vYYYYMMDD-<commit_short_hash>`.
    * `PUBLISH_RELEASE`: true if `--publish` was passed. In this case, the environment
variable `KO_FLAGS` will be updated with the `-L` option.
    * `BRANCH_RELEASE`: true if both `--version` and `--publish-release` were passed.

    All boolean environment variables default to false for safety.

    All environment variables above, except `KO_FLAGS`, are marked read-only once
`initialize()` is called.

### Sample release script

```bash
source vendor/github.com/knative/test-infra/scripts/release.sh

initialize $@

run_validation_tests ./test/presubmit-tests.sh

# config/ contains the manifests
KO_DOCKER_REPO=gcr.io/knative-foo
ko resolve ${KO_FLAGS} -f config/ > release.yaml

tag_images_in_yaml release.yaml $KO_DOCKER_REPO $TAG

if (( PUBLISH_RELEASE )); then
  # gs://knative-foo hosts the manifest
  publish_yaml release.yaml knative-foo $TAG
fi

branch_release "Knative Foo" release.yaml
```
