load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["helloworld.go"],
    importpath = "github.com/knative/serving/sample/service",
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
    name = "service",
    images = {
        "github.com/knative/serving/sample/service": ":image",
    },
    template = ":sample.yaml",
)

k8s_object(
    name = "updated_service",
    images = {
        "github.com/knative/serving/sample/service": ":image",
    },
    template = ":updated_service.yaml",
)

k8s_object(
    name = "pinned_service",
    images = {
        "github.com/knative/serving/sample/service": ":image",
    },
    template = ":pinned_service.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        ":service",
    ],
)

k8s_objects(
    name = "updated_everything",
    objects = [
        ":updated_service",
    ],
)

k8s_objects(
    name = "pinned_everything",
    objects = [
        ":pinned_service",
    ],
)
