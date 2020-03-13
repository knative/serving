#!/usr/bin/env bash

# Copyright 2018 The Knative Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This is a helper script for Knative release scripts.
# See README.md for instructions on how to use it.

source $(dirname ${BASH_SOURCE})/library.sh

# Organization name in GitHub; defaults to Knative.
readonly ORG_NAME="${ORG_NAME:-knative}"

# GitHub upstream.
readonly REPO_UPSTREAM="https://github.com/${ORG_NAME}/${REPO_NAME}"

# GCRs for Knative releases.
readonly NIGHTLY_GCR="gcr.io/knative-nightly/github.com/${ORG_NAME}/${REPO_NAME}"
readonly RELEASE_GCR="gcr.io/knative-releases/github.com/${ORG_NAME}/${REPO_NAME}"

# Georeplicate images to {us,eu,asia}.gcr.io
readonly GEO_REPLICATION=(us eu asia)

# Simple banner for logging purposes.
# Parameters: $1 - message to display.
function banner() {
    make_banner "@" "$1"
}

# Tag images in the yaml files if $TAG is not empty.
# $KO_DOCKER_REPO is the registry containing the images to tag with $TAG.
# Parameters: $1..$n - files to parse for images (non .yaml files are ignored).
function tag_images_in_yamls() {
  [[ -z ${TAG} ]] && return 0
  local SRC_DIR="${GOPATH}/src/"
  local DOCKER_BASE="${KO_DOCKER_REPO}/${REPO_ROOT_DIR/$SRC_DIR}"
  local GEO_REGIONS="${GEO_REPLICATION[@]} "
  echo "Tagging any images under '${DOCKER_BASE}' with ${TAG}"
  for file in $@; do
    [[ "${file##*.}" != "yaml" ]] && continue
    echo "Inspecting ${file}"
    for image in $(grep -o "${DOCKER_BASE}/[a-z\./-]\+@sha256:[0-9a-f]\+" ${file}); do
      for region in "" ${GEO_REGIONS// /. }; do
        gcloud -q container images add-tag ${image} ${region}${image%%@*}:${TAG}
      done
    done
  done
}

# Copy the given files to the $RELEASE_GCS_BUCKET bucket's "latest" directory.
# If $TAG is not empty, also copy them to $RELEASE_GCS_BUCKET bucket's "previous" directory.
# Parameters: $1..$n - files to copy.
function publish_to_gcs() {
  function verbose_gsutil_cp {
    local DEST="gs://${RELEASE_GCS_BUCKET}/$1/"
    shift
    echo "Publishing [$@] to ${DEST}"
    gsutil -m cp $@ ${DEST}
  }
  # Before publishing the files, cleanup the `latest` dir if it exists.
  local latest_dir="gs://${RELEASE_GCS_BUCKET}/latest"
  if [[ -n "$(gsutil ls ${latest_dir} 2> /dev/null)" ]]; then
    echo "Cleaning up '${latest_dir}' first"
    gsutil -m rm ${latest_dir}/**
  fi
  verbose_gsutil_cp latest $@
  [[ -n ${TAG} ]] && verbose_gsutil_cp previous/${TAG} $@
}

# These are global environment variables.
SKIP_TESTS=0
PRESUBMIT_TEST_FAIL_FAST=1
TAG_RELEASE=0
PUBLISH_RELEASE=0
PUBLISH_TO_GITHUB=0
TAG=""
BUILD_COMMIT_HASH=""
BUILD_YYYYMMDD=""
BUILD_TIMESTAMP=""
BUILD_TAG=""
RELEASE_VERSION=""
RELEASE_NOTES=""
RELEASE_BRANCH=""
RELEASE_GCS_BUCKET="knative-nightly/${REPO_NAME}"
RELEASE_DIR=""
KO_FLAGS="-P"
VALIDATION_TESTS="./test/presubmit-tests.sh"
ARTIFACTS_TO_PUBLISH=""
FROM_NIGHTLY_RELEASE=""
FROM_NIGHTLY_RELEASE_GCS=""
export KO_DOCKER_REPO="gcr.io/knative-nightly"
export GITHUB_TOKEN=""

# Convenience function to run the hub tool.
# Parameters: $1..$n - arguments to hub.
function hub_tool() {
  run_go_tool github.com/github/hub hub $@
}

# Shortcut to "git push" that handles authentication.
# Parameters: $1..$n - arguments to "git push <repo>".
function git_push() {
  local repo_url="${REPO_UPSTREAM}"
  [[ -n "${GITHUB_TOKEN}}" ]] && repo_url="${repo_url/:\/\//:\/\/${GITHUB_TOKEN}@}"
  git push ${repo_url} $@
}

# Return the master version of a release.
# For example, "v0.2.1" returns "0.2"
# Parameters: $1 - release version label.
function master_version() {
  local release="${1//v/}"
  local tokens=(${release//\./ })
  echo "${tokens[0]}.${tokens[1]}"
}

# Return the release build number of a release.
# For example, "v0.2.1" returns "1".
# Parameters: $1 - release version label.
function release_build_number() {
  local tokens=(${1//\./ })
  echo "${tokens[2]}"
}

# Return the short commit SHA from a release tag.
# For example, "v20010101-deadbeef" returns "deadbeef".
function hash_from_tag() {
  local tokens=(${1//-/ })
  echo "${tokens[1]}"
}

# Setup the repository upstream, if not set.
function setup_upstream() {
  # hub and checkout need the upstream URL to be set
  # TODO(adrcunha): Use "git remote get-url" once available on Prow.
  local upstream="$(git config --get remote.upstream.url)"
  echo "Remote upstream URL is '${upstream}'"
  if [[ -z "${upstream}" ]]; then
    echo "Setting remote upstream URL to '${REPO_UPSTREAM}'"
    git remote add upstream ${REPO_UPSTREAM}
  fi
}

# Fetch the release branch, so we can check it out.
function setup_branch() {
  [[ -z "${RELEASE_BRANCH}" ]] && return
  git fetch ${REPO_UPSTREAM} ${RELEASE_BRANCH}:upstream/${RELEASE_BRANCH}
}

# Setup version, branch and release notes for a auto release.
function prepare_auto_release() {
  echo "Auto release requested"
  TAG_RELEASE=1
  PUBLISH_RELEASE=1

  git fetch --all || abort "error fetching branches/tags from remote"
  local tags="$(git tag | cut -d 'v' -f2 | cut -d '.' -f1-2 | sort -V | uniq)"
  local branches="$( { (git branch -r | grep upstream/release-) ; (git branch | grep release-); } | cut -d '-' -f2 | sort -V | uniq)"

  echo "Versions released (from tags): [" ${tags} "]"
  echo "Versions released (from branches): [" ${branches} "]"

  local release_number=""
  for i in ${branches}; do
    release_number="${i}"
    for j in ${tags}; do
      if [[ "${i}" == "${j}" ]]; then
        release_number=""
      fi
    done
  done

  if [[ -z "${release_number}" ]]; then
    echo "*** No new release will be generated, as no new branches exist"
    exit  0
  fi

  RELEASE_VERSION="${release_number}.0"
  RELEASE_BRANCH="release-${release_number}"
  echo "Will create release ${RELEASE_VERSION} from branch ${RELEASE_BRANCH}"
  # If --release-notes not used, add a placeholder
  if [[ -z "${RELEASE_NOTES}" ]]; then
    RELEASE_NOTES="$(mktemp)"
    echo "[add release notes here]" > ${RELEASE_NOTES}
  fi
}

# Setup version, branch and release notes for a "dot" release.
function prepare_dot_release() {
  echo "Dot release requested"
  TAG_RELEASE=1
  PUBLISH_RELEASE=1
  git fetch --all || abort "error fetching branches/tags from remote"
  # List latest release
  local releases # don't combine with the line below, or $? will be 0
  releases="$(hub_tool release)"
  [[ $? -eq 0 ]] || abort "cannot list releases"
  # If --release-branch passed, restrict to that release
  if [[ -n "${RELEASE_BRANCH}" ]]; then
    local version_filter="v${RELEASE_BRANCH##release-}"
    echo "Dot release will be generated for ${version_filter}"
    releases="$(echo "${releases}" | grep ^${version_filter})"
  fi
  local last_version="$(echo "${releases}" | grep '^v[0-9]\+\.[0-9]\+\.[0-9]\+$' | sort -r -V | head -1)"
  [[ -n "${last_version}" ]] || abort "no previous release exist"
  local major_minor_version=""
  if [[ -z "${RELEASE_BRANCH}" ]]; then
    echo "Last release is ${last_version}"
    # Determine branch
    major_minor_version="$(master_version ${last_version})"
    RELEASE_BRANCH="release-${major_minor_version}"
    echo "Last release branch is ${RELEASE_BRANCH}"
  else
    major_minor_version="${RELEASE_BRANCH##release-}"
  fi
  [[ -n "${major_minor_version}" ]] || abort "cannot get release major/minor version"
  # Ensure there are new commits in the branch, otherwise we don't create a new release
  setup_branch
  local last_release_commit="$(git rev-list -n 1 ${last_version})"
  local release_branch_commit="$(git rev-list -n 1 upstream/${RELEASE_BRANCH})"
  [[ -n "${last_release_commit}" ]] || abort "cannot get last release commit"
  [[ -n "${release_branch_commit}" ]] || abort "cannot get release branch last commit"
  echo "Version ${last_version} is at commit ${last_release_commit}"
  echo "Branch ${RELEASE_BRANCH} is at commit ${release_branch_commit}"
  if [[ "${last_release_commit}" == "${release_branch_commit}" ]]; then
    echo "*** Branch ${RELEASE_BRANCH} has no new cherry-picks since release ${last_version}"
    echo "*** No dot release will be generated, as no changes exist"
    exit 0
  fi
  # Create new release version number
  local last_build="$(release_build_number ${last_version})"
  RELEASE_VERSION="${major_minor_version}.$(( last_build + 1 ))"
  echo "Will create release ${RELEASE_VERSION} at commit ${release_branch_commit}"
  # If --release-notes not used, copy from the latest release
  if [[ -z "${RELEASE_NOTES}" ]]; then
    RELEASE_NOTES="$(mktemp)"
    hub_tool release show -f "%b" ${last_version} > ${RELEASE_NOTES}
    echo "Release notes from ${last_version} copied to ${RELEASE_NOTES}"
  fi
}

# Setup source nightly image for a release.
function prepare_from_nightly_release() {
  echo "Release from nightly requested"
  SKIP_TESTS=1
  if [[ "${FROM_NIGHTLY_RELEASE}" == "latest" ]]; then
    echo "Finding the latest nightly release"
    find_latest_nightly "${NIGHTLY_GCR}" || abort "cannot find the latest nightly release"
    echo "Latest nightly is ${FROM_NIGHTLY_RELEASE}"
  fi
  readonly FROM_NIGHTLY_RELEASE_GCS="gs://knative-nightly/${REPO_NAME}/previous/${FROM_NIGHTLY_RELEASE}"
  gsutil ls -d "${FROM_NIGHTLY_RELEASE_GCS}" > /dev/null \
      || abort "nightly release ${FROM_NIGHTLY_RELEASE} doesn't exist"
}

# Build a release from an existing nightly one.
function build_from_nightly_release() {
  banner "Building the release"
  echo "Fetching manifests from nightly"
  local yamls_dir="$(mktemp -d)"
  gsutil -m cp -r "${FROM_NIGHTLY_RELEASE_GCS}/*" "${yamls_dir}" || abort "error fetching manifests"
  # Update references to release GCR
  for yaml in ${yamls_dir}/*.yaml; do
    sed -i -e "s#${NIGHTLY_GCR}#${RELEASE_GCR}#" "${yaml}"
  done
  ARTIFACTS_TO_PUBLISH="$(find ${yamls_dir} -name '*.yaml' -printf '%p ')"
  echo "Copying nightly images"
  copy_nightly_images_to_release_gcr "${NIGHTLY_GCR}" "${FROM_NIGHTLY_RELEASE}"
  # Create a release branch from the nightly release tag.
  local commit="$(hash_from_tag ${FROM_NIGHTLY_RELEASE})"
  echo "Creating release branch ${RELEASE_BRANCH} at commit ${commit}"
  git checkout -b ${RELEASE_BRANCH} ${commit} || abort "cannot create branch"
  git_push upstream ${RELEASE_BRANCH} || abort "cannot push branch"
}

# Build a release from source.
function build_from_source() {
  run_validation_tests ${VALIDATION_TESTS}
  banner "Building the release"
  build_release
  # Do not use `||` above or any error will be swallowed.
  if [[ $? -ne 0 ]]; then
    abort "error building the release"
  fi
}

# Copy tagged images from the nightly GCR to the release GCR, tagging them 'latest'.
# This is a recursive function, first call must pass $NIGHTLY_GCR as first parameter.
# Parameters: $1 - GCR to recurse into.
#             $2 - tag to be used to select images to copy.
function copy_nightly_images_to_release_gcr() {
  for entry in $(gcloud --format="value(name)" container images list --repository="$1"); do
    copy_nightly_images_to_release_gcr "${entry}" "$2"
    # Copy each image with the given nightly tag
    for x in $(gcloud --format="value(tags)" container images list-tags "${entry}" --filter="tags=$2" --limit=1); do
      local path="${entry/${NIGHTLY_GCR}}"  # Image "path" (remove GCR part)
      local dst="${RELEASE_GCR}${path}:latest"
      gcloud container images add-tag "${entry}:$2" "${dst}" || abort "error copying image"
    done
  done
}

# Recurse into GCR and find the nightly tag of the first `latest` image found.
# Parameters: $1 - GCR to recurse into.
function find_latest_nightly() {
  for entry in $(gcloud --format="value(name)" container images list --repository="$1"); do
    find_latest_nightly "${entry}" && return 0
    for tag in $(gcloud --format="value(tags)" container images list-tags "${entry}" \
        --filter="tags=latest" --limit=1); do
      local tags=( ${tag//,/ } )
      # Skip if more than one nightly tag, as we don't know what's the latest.
      if [[ ${#tags[@]} -eq 2 ]]; then
        local nightly_tag="${tags[@]/latest}"  # Remove 'latest' tag
        FROM_NIGHTLY_RELEASE="${nightly_tag// /}"  # Remove spaces
        return 0
      fi
    done
  done
  return 1
}

# Parses flags and sets environment variables accordingly.
function parse_flags() {
  local has_gcr_flag=0
  local has_gcs_flag=0
  local has_dir_flag=0
  local is_dot_release=0
  local is_auto_release=0

  cd ${REPO_ROOT_DIR}
  while [[ $# -ne 0 ]]; do
    local parameter=$1
    case ${parameter} in
      --skip-tests) SKIP_TESTS=1 ;;
      --tag-release) TAG_RELEASE=1 ;;
      --notag-release) TAG_RELEASE=0 ;;
      --publish) PUBLISH_RELEASE=1 ;;
      --nopublish) PUBLISH_RELEASE=0 ;;
      --dot-release) is_dot_release=1 ;;
      --auto-release) is_auto_release=1 ;;
      --from-latest-nightly) FROM_NIGHTLY_RELEASE=latest ;;
      *)
        [[ $# -ge 2 ]] || abort "missing parameter after $1"
        shift
        case ${parameter} in
          --github-token)
            [[ ! -f "$1" ]] && abort "file $1 doesn't exist"
            # Remove any trailing newline/space from token
            GITHUB_TOKEN="$(echo -n $(cat $1))"
            [[ -n "${GITHUB_TOKEN}" ]] || abort "file $1 is empty"
            ;;
          --release-gcr)
            KO_DOCKER_REPO=$1
            has_gcr_flag=1
            ;;
          --release-gcs)
            RELEASE_GCS_BUCKET=$1
            RELEASE_DIR=""
            has_gcs_flag=1
            ;;
          --release-dir)
            RELEASE_DIR=$1
            RELEASE_GCS_BUCKET=""
            has_dir_flag=1
            ;;
          --version)
            [[ $1 =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || abort "version format must be '[0-9].[0-9].[0-9]'"
            RELEASE_VERSION=$1
            ;;
          --branch)
            [[ $1 =~ ^release-[0-9]+\.[0-9]+$ ]] || abort "branch name must be 'release-[0-9].[0-9]'"
            RELEASE_BRANCH=$1
            ;;
          --release-notes)
            [[ ! -f "$1" ]] && abort "file $1 doesn't exist"
            RELEASE_NOTES=$1
            ;;
          --from-nightly)
            [[ $1 =~ ^v[0-9]+-[0-9a-f]+$ ]] || abort "nightly tag must be 'vYYYYMMDD-commithash'"
            FROM_NIGHTLY_RELEASE=$1
            ;;
          *) abort "unknown option ${parameter}" ;;
        esac
    esac
    shift
  done

  (( has_gcs_flag )) && (( has_dir_flag )) && abort "cannot have both --release-gcs and --release-dir set simultaneously"
  [[ -n "${RELEASE_GCS_BUCKET}" && -n "${RELEASE_DIR}" ]] && abort "cannot have both GCS and release directory set"

  # Do auto release unless release is forced
  if (( is_auto_release )); then
    (( is_dot_release )) && abort "cannot have both --dot-release and --auto-release set simultaneously"
    [[ -n "${RELEASE_VERSION}" ]] && abort "cannot have both --version and --auto-release set simultaneously"
    [[ -n "${RELEASE_BRANCH}" ]] && abort "cannot have both --branch and --auto-release set simultaneously"
    [[ -n "${FROM_NIGHTLY_RELEASE}" ]] && abort "cannot have --auto-release with a nightly source"
    setup_upstream
    prepare_auto_release
  fi

  # Setup source nightly image
  if [[ -n "${FROM_NIGHTLY_RELEASE}" ]]; then
    (( is_dot_release )) && abort "dot releases are built from source"
    [[ -z "${RELEASE_VERSION}" ]] && abort "release version must be specified with --version"
    # TODO(adrcunha): "dot" releases from release branches require releasing nightlies
    # for such branches, which we don't do yet.
    [[ "${RELEASE_VERSION}" =~ ^[0-9]+\.[0-9]+\.0$ ]] || abort "version format must be 'X.Y.0'"
    RELEASE_BRANCH="release-$(master_version ${RELEASE_VERSION})"
    prepare_from_nightly_release
    setup_upstream
  fi

  # Setup dot releases
  if (( is_dot_release )); then
    setup_upstream
    prepare_dot_release
  fi

  # Update KO_DOCKER_REPO and KO_FLAGS if we're not publishing.
  if (( ! PUBLISH_RELEASE )); then
    (( has_gcr_flag )) && echo "Not publishing the release, GCR flag is ignored"
    (( has_gcs_flag )) && echo "Not publishing the release, GCS flag is ignored"
    KO_DOCKER_REPO="ko.local"
    RELEASE_GCS_BUCKET=""
    [[ -z "${RELEASE_DIR}" ]] && RELEASE_DIR="${REPO_ROOT_DIR}"
  fi

  [[ -z "${RELEASE_GCS_BUCKET}" && -z "${RELEASE_DIR}" ]] && abort "--release-gcs or --release-dir must be used"
  if [[ -n "${RELEASE_DIR}" ]]; then
    mkdir -p "${RELEASE_DIR}" || abort "cannot create release dir '${RELEASE_DIR}'"
  fi

  # Get the commit, excluding any tags but keeping the "dirty" flag
  BUILD_COMMIT_HASH="$(git describe --always --dirty --match '^$')"
  [[ -n "${BUILD_COMMIT_HASH}" ]] || abort "error getting the current commit"
  BUILD_YYYYMMDD="$(date -u +%Y%m%d)"
  BUILD_TIMESTAMP="$(date -u '+%Y-%m-%d %H:%M:%S')"
  BUILD_TAG="v${BUILD_YYYYMMDD}-${BUILD_COMMIT_HASH}"

  (( TAG_RELEASE )) && TAG="${BUILD_TAG}"
  [[ -n "${RELEASE_VERSION}" ]] && TAG="v${RELEASE_VERSION}"
  [[ -n "${RELEASE_VERSION}" && -n "${RELEASE_BRANCH}" ]] && (( PUBLISH_RELEASE )) && PUBLISH_TO_GITHUB=1

  readonly BUILD_COMMIT_HASH
  readonly BUILD_YYYYMMDD
  readonly BUILD_TIMESTAMP
  readonly BUILD_TAG
  readonly SKIP_TESTS
  readonly TAG_RELEASE
  readonly PUBLISH_RELEASE
  readonly PUBLISH_TO_GITHUB
  readonly TAG
  readonly RELEASE_VERSION
  readonly RELEASE_NOTES
  readonly RELEASE_BRANCH
  readonly RELEASE_GCS_BUCKET
  readonly RELEASE_DIR
  readonly KO_DOCKER_REPO
  readonly VALIDATION_TESTS
  readonly FROM_NIGHTLY_RELEASE
}

# Run tests (unless --skip-tests was passed). Conveniently displays a banner indicating so.
# Parameters: $1 - executable that runs the tests.
function run_validation_tests() {
  (( SKIP_TESTS )) && return
  banner "Running release validation tests"
  # Run tests.
  if ! $1; then
    banner "Release validation tests failed, aborting"
    abort "release validation tests failed"
  fi
}

# Publishes the generated artifacts to directory, GCS, GitHub, etc.
# Parameters: $1..$n - files to add to the release.
function publish_artifacts() {
  (( ! PUBLISH_RELEASE )) && return
  tag_images_in_yamls ${ARTIFACTS_TO_PUBLISH}
  if [[ -n "${RELEASE_DIR}" ]]; then
    cp ${ARTIFACTS_TO_PUBLISH} ${RELEASE_DIR} || abort "cannot copy release to '${RELEASE_DIR}'"
  fi
  [[ -n "${RELEASE_GCS_BUCKET}" ]] && publish_to_gcs ${ARTIFACTS_TO_PUBLISH}
  publish_to_github ${ARTIFACTS_TO_PUBLISH}
  banner "New release published successfully"
}

# Entry point for a release script.
function main() {
  parse_flags "$@"

  # Checkout specific branch, if necessary
  local current_branch
  current_branch="$(git rev-parse --abbrev-ref HEAD)"
  if [[ -n "${RELEASE_BRANCH}" && -z "${FROM_NIGHTLY_RELEASE}" && "${current_branch}" != "${RELEASE_BRANCH}" ]]; then
    setup_upstream
    setup_branch
    # When it runs in Prow, the origin is identical with upstream, and previous
    # fetch already fetched release-* branches, so no need to `checkout -b`
    if (( IS_PROW )); then
      git checkout "${RELEASE_BRANCH}" || abort "cannot checkout branch ${RELEASE_BRANCH}"
    else
      git checkout -b "${RELEASE_BRANCH}" upstream/"${RELEASE_BRANCH}" || abort "cannot checkout branch ${RELEASE_BRANCH}"
    fi
    # HACK HACK HACK
    # Rerun the release script from the release branch. Fixes https://github.com/knative/test-infra/issues/1262
    ./hack/release.sh "$@"
    exit "$?"
  fi

  function_exists build_release || abort "function 'build_release()' not defined"
  [[ -x ${VALIDATION_TESTS} ]] || abort "test script '${VALIDATION_TESTS}' doesn't exist"

  # Log what will be done and where.
  banner "Release configuration"
  if which gcloud &>/dev/null ; then
    echo "- gcloud user: $(gcloud config get-value core/account)"
  fi
  echo "- Go path: ${GOPATH}"
  echo "- Repository root: ${REPO_ROOT_DIR}"
  echo "- Destination GCR: ${KO_DOCKER_REPO}"
  (( SKIP_TESTS )) && echo "- Tests will NOT be run" || echo "- Tests will be run"
  if (( TAG_RELEASE )); then
    echo "- Artifacts will be tagged '${TAG}'"
  else
    echo "- Artifacts WILL NOT be tagged"
  fi
  if (( PUBLISH_RELEASE )); then
    local dst="${RELEASE_DIR}"
    [[ -z "${dst}" ]] && dst="${RELEASE_GCS_BUCKET}"
    echo "- Release WILL BE published to '${dst}'"
  else
    echo "- Release will not be published"
  fi
  if (( PUBLISH_TO_GITHUB )); then
    echo "- Release WILL BE published to GitHub"
  fi
  if [[ -n "${FROM_NIGHTLY_RELEASE}" ]]; then
    echo "- Release will be A COPY OF '${FROM_NIGHTLY_RELEASE}' nightly"
  else
    echo "- Release will be BUILT FROM SOURCE"
    [[ -n "${RELEASE_BRANCH}" ]] && echo "- Release will be built from branch '${RELEASE_BRANCH}'"
  fi
  [[ -n "${RELEASE_NOTES}" ]] && echo "- Release notes are generated from '${RELEASE_NOTES}'"

  if [[ -n "${FROM_NIGHTLY_RELEASE}" ]]; then
    build_from_nightly_release
  else
    set -e -o pipefail
    build_from_source
    set +e +o pipefail
  fi
  [[ -z "${ARTIFACTS_TO_PUBLISH}" ]] && abort "no artifacts were generated"
  # Ensure no empty file will be published.
  for artifact in ${ARTIFACTS_TO_PUBLISH}; do
    [[ -s ${artifact} ]] || abort "Artifact ${artifact} is empty"
  done
  echo "New release built successfully"
  publish_artifacts
}

# Publishes a new release on GitHub, also git tagging it (unless this is not a versioned release).
# Parameters: $1..$n - files to add to the release.
function publish_to_github() {
  (( PUBLISH_TO_GITHUB )) || return 0
  local title="${REPO_NAME_FORMATTED} release ${TAG}"
  local attachments=()
  local description="$(mktemp)"
  local attachments_dir="$(mktemp -d)"
  local commitish=""
  # Copy files to a separate dir
  for artifact in $@; do
    cp ${artifact} ${attachments_dir}/
    attachments+=("--attach=${artifact}#$(basename ${artifact})")
  done
  echo -e "${title}\n" > ${description}
  if [[ -n "${RELEASE_NOTES}" ]]; then
    cat ${RELEASE_NOTES} >> ${description}
  fi
  git tag -a ${TAG} -m "${title}"
  git_push tag ${TAG}

  [[ -n "${RELEASE_BRANCH}" ]] && commitish="--commitish=${RELEASE_BRANCH}"
  for i in {2..0}; do
    hub_tool release create \
        --prerelease \
        ${attachments[@]} \
        --file=${description} \
        ${commitish} \
        ${TAG} && return 0
    if [[ "${i}" -gt 0 ]]; then
      echo "Error publishing the release, retrying in 15s..."
      sleep 15
    fi
  done
  abort "Cannot publish release to GitHub"
}
