load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["autoscale.go"],
    importpath = "github.com/knative/serving/sample/autoscale",
    visibility = ["//visibility:private"],
)

go_binary(
    name = "autoscale",
    embed = [":go_default_library"],
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
    name = "everything",
    images = {
        "github.com/knative/serving/sample/autoscale": ":image",
    },
    template = ":sample.yaml",
)
