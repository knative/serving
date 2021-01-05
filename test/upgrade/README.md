# Upgrade Tests

In order to get coverage for the upgrade process from an operator’s perspective,
we need an additional suite of tests that perform a complete knative upgrade.
Running these tests on every commit will ensure that we don’t introduce any
non-upgradeable changes, so every commit should be releasable.

This is inspired by kubernetes
[upgrade testing](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-testing/e2e-tests.md#version-skewed-and-upgrade-testing).

These tests are a pretty big hammer in that they cover more than just version
changes, but it’s one of the only ways to make sure we don’t accidentally make
breaking changes for now.

## Flow

We’d like to validate that the upgrade doesn’t break any resources (they still
respond to requests) and doesn't break our installation (we can still update
resources).

At a high level, we want to do this:

1. Install the latest knative release.
1. Create some resources.
1. Install knative at HEAD.
1. Test those resources, verify that we didn’t break anything.

To achieve that, we utilize the
[upgrade test framework](https://github.com/knative/pkg/tree/master/test/upgrade).
The framework runs tests in the following phases:

1. Install the latest release from GitHub.
1. Run the `PreUpgrade` tests.
1. Start the `Continual` tests.
1. Install from the HEAD of the master branch.
1. Run the `PostUpgrade` tests.
1. Install the latest release from GitHub.
1. Run the `PostDowngrade` tests.
1. End the `Continual` tests and collect results.

## Tests

#### PreUpgrade

Create a Service pointing to `image1`, ensure it responds correctly.

#### PostUpgrade

Ensure the Service still responds correctly after upgrading. Update it to point
to `image2`, ensure it responds correctly. Ensure that a new service can be
created after downgrade.

#### PostDowngrade

Ensure the Service still responds correctly after downgrading. Update it to
point back to `image1`, ensure it responds correctly. Ensure that a new service
can be created after downgrade.

### Continual

In order to verify that we don't have data-plane unavailability during our
control-plane outages (when we're upgrading the knative/serving installation),
we run a prober test that continually sends requests to a service during the
entire upgrade process. When the upgrade completes, we make sure that none of
those requests failed. We also run autoscale tests to ensure that autoscaling
works correctly during upgrades/downgrades.
