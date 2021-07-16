# Quickstore library for Go.
# See quickstore.go and go/mako-quickstore for documentation.
# See quickstore_example_test.go for example usage.
package(default_visibility = ["//visibility:public"])

licenses(["notice"])

load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "quickstore",
    srcs = ["quickstore.go"],
    importpath = "github.com/google/mako/go/quickstore",
    deps = [
        "//internal/go/common",
        "//internal/quickstore_microservice/proto:quickstore_go_grpc_pb",
        "//internal/quickstore_microservice/proto:quickstore_go_proto",
        "//proto/quickstore:quickstore_go_proto",
        "//spec/proto:mako_go_proto",
        "@com_github_golang_glog//:go_default_library",
        "@com_github_golang_protobuf//proto:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
    ],
)
