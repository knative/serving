# Creating a new Knative Serving release

The `release.sh` script automates the creation of Knative Serving releases,
either nightly or versioned ones.

By default, the script creates a nightly release but does not publish anywhere.

## Common flags for cutting releases

The following flags affect the behavior of the script, no matter the type of
the release.

* `--skip-tests` Do not run tests before building the release. Otherwise,
build, unit and end-to-end tests are run and they all must pass for the
release to be built.
* `--tag-release`, `--notag-release` Tag (or not) the generated images
with either `vYYYYMMDD-<commit_short_hash>` (for nightly releases) or
`vX.Y.Z` for versioned releases. *For versioned releases, a tag is always
added.*
* `--publish`, `--nopublish` Whether the generated images should be published
to a GCR, and the generated manifests written to a GCS bucket or not. If yes,
the destination GCR is defined by the environment variable
`$SERVING_RELEASE_GCR` (defaults to `gcr.io/knative-releases`) and the
destination GCS bucket is defined by the environment variable
`$SERVING_RELEASE_GCS` (defaults to `knative-releases/serving`). If no, the
images will be pushed to the `ko.local` registry, and the manifests written
to the local disk only (in the repository root directory).

## Creating nightly releases

Nightly releases are built against the current git tree. The behavior of the
script is defined by the common flags. You must have write access to the GCR
and GCS bucket the release will be pushed to, unless `--nopublish` is used.

Examples:

```bash
# Create and publish a nightly, tagged release.
./hack/release.sh --publish --tag-release

# Create release, but don't test, publish or tag it.
./hack/release.sh --skip-tests --nopublish --notag-release
```

## Creating versioned releases

*Note: only Knative admins can create versioned releases.*

To specify a versioned release to be cut, you must use the `--version` flag.
Versioned releases are usually built against a branch in the Knative Serving
repository, specified by the `--branch` flag. 

* `--version` Defines the version of the release, and must be in the form
`X.Y.Z`, where X, Y and Z are numbers.
* `--branch` Defines the branch in Knative Serving repository from which the
release will be built. If not passed, the `master` branch at HEAD will be
used. This branch must be created before the script is executed, and must be
in the form `release-X.Y`, where X and Y must match the numbers used in the
version passed in the `--version` flag. This flag has no effect unless
`--version` is also passed.
* `--release-notes` Points to a markdown file containing a description of the
release. This is optional but highly recommended. It has no effect unless
`--version` is also passed.

If this is the first time you're cutting a versioned release, you'll be prompted
for your GitHub username, password, and possibly 2-factor authentication
challenge before the release is published.

The release will be published in the *Releases* page of the Knative Serving
repository, with the title *Knative Serving release vX.Y.Z* and the given
release notes. It will also be tagged *vX.Y.Z* (both on GitHub and as a git
annotated tag).
