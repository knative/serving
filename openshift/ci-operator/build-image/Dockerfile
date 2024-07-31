# Dockerfile to bootstrap build and test in openshift-ci

FROM registry.ci.openshift.org/openshift/release:rhel-8-release-golang-1.22-openshift-4.17 as builder

# Add kubernetes repository
ADD openshift/ci-operator/build-image/kubernetes.repo /etc/yum.repos.d/

RUN yum install -y kubectl httpd-tools

# Serverless-Operator `make generated-files` needs helm
RUN curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash

RUN GOFLAGS='' go install github.com/mikefarah/yq/v3@latest
RUN GOFLAGS='' go install knative.dev/test-infra/tools/kntest/cmd/kntest@latest

# go install creates $GOPATH/.cache with root permissions, we delete it here
# to avoid permission issues with the runtime users
RUN rm -rf $GOPATH/.cache

# Allow runtime users to add entries to /etc/passwd
RUN chmod g+rw /etc/passwd
