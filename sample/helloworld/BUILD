load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["helloworld.go"],
    importpath = "github.com/elafros/elafros/sample/helloworld",
    visibility = ["//visibility:private"],
    deps = ["//vendor/github.com/golang/glog:go_default_library"],
)

go_binary(
    name = "helloworld",
    embed = [":go_default_library"],
    pure = "on",
    visibility = ["//visibility:public"],
)

load("@io_bazel_rules_docker//go:image.bzl", "go_image")

go_image(
    name = "image",
    binary = ":helloworld",
)

load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "configuration",
    images = {
        "sample:latest": ":image",
    },
    template = ":configuration.yaml",
)

k8s_object(
    name = "updated_configuration",
    images = {
        "sample:latest": ":image",
    },
    template = ":updated_configuration.yaml",
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

k8s_objects(
    name = "updated_everything",
    objects = [
        ":route",
        ":updated_configuration",
    ],
)
