# Copyright 2022 The OpenZipkin Authors
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

.DEFAULT_GOAL := test

.PHONY: test
test:
	# MallocNanoZone env var avoids problems in macOS Monterey: golang/go#49138
	MallocNanoZone=0 go test -v -race -cover ./...

.PHONY: bench
bench:
	go test -v -run - -bench . -benchmem ./...

.PHONY: protoc
protoc:
	protoc --go_out=module=github.com/openzipkin/zipkin-go:. proto/zipkin_proto3/zipkin.proto
	protoc --go_out=module=github.com/openzipkin/zipkin-go:. proto/testing/*.proto
	protoc --go-grpc_out=module=github.com/openzipkin/zipkin-go:. proto/testing/*.proto

.PHONY: lint
lint:
	# Ignore grep's exit code since no match returns 1.
	echo 'linting...' ; golint ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: all
all: vet lint test bench

.PHONY: example
