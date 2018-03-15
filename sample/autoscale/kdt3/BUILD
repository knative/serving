load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["main.go"],
    importpath = "github.com/elafros/elafros/sample/autoscale/kdt3",
    visibility = ["//visibility:private"],
    deps = ["//sample/autoscale/kdt3/app:go_default_library"],
)

go_binary(
    name = "kdt3",
    embed = [":go_default_library"],
    importpath = "github.com/elafros/elafros/sample/autoscale/kdt3",
    pure = "on",
    visibility = ["//visibility:public"],
)

load("@io_bazel_rules_docker//go:image.bzl", "go_image")

go_image(
    name = "image",
    binary = ":kdt3",
)

load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "configuration",
    images = {
        "autoscale-kdt3-sample:latest": ":image",
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
