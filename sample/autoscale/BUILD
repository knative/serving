load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["autoscale.go"],
    importpath = "github.com/google/elafros/sample/autoscale",
    visibility = ["//visibility:private"],
    deps = ["//vendor/github.com/gorilla/mux:go_default_library"],
)

go_binary(
    name = "autoscale",
    embed = [":go_default_library"],
    importpath = "github.com/google/elafros/sample/autoscale",
    pure = "on",
    visibility = ["//visibility:public"],
)

load("@io_bazel_rules_docker//go:image.bzl", "go_image")

go_image(
    name = "image",
    binary = ":autoscale",
)

load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "configuration",
    images = {
        "autoscale-sample:latest": ":image",
    },
    template = ":configuration.yaml",
)

k8s_object(
    name = "route",
    template = ":route.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        ":route",
        ":configuration",
    ],
)
