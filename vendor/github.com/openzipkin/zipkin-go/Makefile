
.DEFAULT_GOAL := test

.PHONY: test
test:
	go test -v -race -cover ./...

.PHONY: bench
bench:
	go test -v -run - -bench . -benchmem ./...

.PHONY: protoc
protoc:
	protoc --go_out=module=github.com/openzipkin/zipkin-go:. proto/zipkin_proto3/zipkin.proto
	protoc --go_out=module=github.com/openzipkin/zipkin-go:. proto/testing/service.proto
	protoc --go-grpc_out=module=github.com/openzipkin/zipkin-go:. proto/testing/service.proto

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
