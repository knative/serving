load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["helloworld.go"],
    importpath = "github.com/knative/serving/sample/helloworld",
    visibility = ["//visibility:private"],
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
    name = "everything",
    images = {
        "github.com/knative/serving/sample/helloworld": ":image",
    },
    template = ":sample.yaml",
)

k8s_object(
    name = "updated_configuration",
    images = {
        "github.com/knative/serving/sample/helloworld": ":image",
    },
    template = ":updated_configuration.yaml",
)
