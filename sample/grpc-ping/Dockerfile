# Copyright 2018 The Knative Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM golang AS builder
ARG SAMPLE
ARG BUILDTAG

# Get the dependencies from GitHub
RUN go get google.golang.org/grpc

WORKDIR /go/src/github.com/knative/serving
ADD . /go/src/github.com/knative/serving

RUN CGO_ENABLED=0 go build -tags=${BUILDTAG} ./sample/${SAMPLE}

FROM gcr.io/distroless/base
ARG SAMPLE

EXPOSE 8080
COPY --from=builder /go/src/github.com/knative/serving/${SAMPLE} /sample

ENTRYPOINT ["/sample"]
