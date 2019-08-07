# Copyright 2019 The Knative Authors
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

FROM gcr.io/k8s-testimages/kubekins-e2e:latest-master
LABEL maintainer "Srinivas Hegde<srinivashegde@google.com>"

ENV DEBIAN_FRONTEND noninteractive
ENV KO_DOCKER_REPO gcr.io/knative-performance
ENV GOPATH /go

RUN apt-get update
RUN gcloud components update

# Docker
RUN gcloud auth configure-docker

# Extra tools through go get
RUN go get -u github.com/google/ko/cmd/ko
RUN go get -u github.com/golang/dep/cmd/dep

# Add project root folder
ADD . /go/src/knative.dev/serving/test/performance/
