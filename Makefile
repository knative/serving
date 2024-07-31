#This makefile is used by ci-operator

CGO_ENABLED=0
GOOS=linux
CORE_IMAGES=./cmd/activator ./cmd/autoscaler ./cmd/autoscaler-hpa ./cmd/controller ./cmd/queue ./cmd/webhook ./vendor/knative.dev/pkg/apiextensions/storageversion/cmd/migrate
TEST_IMAGES=$(shell find ./test/test_images ./test/test_images/multicontainer -mindepth 1 -maxdepth 1 -type d)
# Exclude wrapper images like multicontainer and initcontainers as those are just ko convenience wrappers used upstream. The openshift serverless tests use other images to run.
TEST_IMAGES_WITHOUT_WRAPPERS=$(shell find ./test/test_images ./test/test_images/multicontainer -mindepth 1 -maxdepth 1 -type d -not -name multicontainer -not -name initcontainers)
BRANCH=
TEST=
IMAGE=
TEST_IMAGE_TAG ?= latest

# Guess location of openshift/release repo. NOTE: override this if it is not correct.
OPENSHIFT=${CURDIR}/../../github.com/openshift/release

install:
	for img in $(CORE_IMAGES); do \
		go install -tags="disable_gcp,disable_aws,disable_azure" $$img ; \
	done
.PHONY: install

test-install:
	for img in $(TEST_IMAGES_WITHOUT_WRAPPERS); do \
		go install $$img ; \
	done
.PHONY: test-install

test-e2e:
	./openshift/e2e-tests.sh
.PHONY: test-e2e

perf-tests:
	export ES_DEVELOPMENT=true && \
	export ES_HOST_PORT=search-perfscale-dev-chmf5l4sh66lvxbnadi4bznl3a.us-west-2.es.amazonaws.com && \
	export USE_OPEN_SEARCH=true && \
	export SYSTEM_NAMESPACE=knative-serving && \
	./openshift/perf-tests.sh
.PHONY: perf-tests

test-e2e-tls:
	ENABLE_INTERNAL_TLS="true" ./openshift/e2e-tests.sh
.PHONY: test-e2e-tls

# Target used by github actions.
test-images:
	for img in $(TEST_IMAGES); do \
		KO_DOCKER_REPO=$(DOCKER_REPO_OVERRIDE) ko build --tags=$(TEST_IMAGE_TAG) $(KO_FLAGS) -B $$img || \
		KO_DOCKER_REPO=$(DOCKER_REPO_OVERRIDE) ko resolve --tags=$(TEST_IMAGE_TAG) $(KO_FLAGS) -RBf $$img || exit $?; \
	done
.PHONY: test-images

test-image-single:
	KO_DOCKER_REPO=$(DOCKER_REPO_OVERRIDE) ko build --tags=$(TEST_IMAGE_TAG) $(KO_FLAGS) -B test/test_images/$(IMAGE) || \
	KO_DOCKER_REPO=$(DOCKER_REPO_OVERRIDE) ko resolve --tags=$(TEST_IMAGE_TAG) $(KO_FLAGS) -RBf test/test_images/$(IMAGE)
.PHONY: test-image-single

# Run make DOCKER_REPO_OVERRIDE=<your_repo> test-e2e-local if test images are available
# in the given repository. Make sure you first build and push them there by running `make test-images`.
# Run make BRANCH=<ci_promotion_name> test-e2e-local if test images from the latest CI
# build for this branch should be used. Example: `make BRANCH=knative-v0.13.2 test-e2e-local`.
# If neither DOCKER_REPO_OVERRIDE nor BRANCH are defined the tests will use test images
# from the last nightly build.
# If TEST is defined then only the single test will be run.
test-e2e-local:
	./openshift/e2e-tests-local.sh $(TEST)
.PHONY: test-e2e-local

# Generate Dockerfiles for core and test images used by ci-operator. The files need to be committed manually.
generate-dockerfiles:
	./openshift/ci-operator/generate-dockerfiles.sh openshift/ci-operator/knative-images $(CORE_IMAGES)
	./openshift/ci-operator/generate-dockerfiles.sh openshift/ci-operator/knative-test-images $(TEST_IMAGES_WITHOUT_WRAPPERS)
.PHONY: generate-dockerfiles

# Generate an aggregated knative yaml file with replaced image references
generate-release:
	./openshift/release/generate-release.sh $(RELEASE)
.PHONY: generate-release
