load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["helloworld.go"],
    importpath = "github.com/google/elafros/sample/helloworld",
    visibility = ["//visibility:private"],
)

go_binary(
    name = "helloworld",
    embed = [":go_default_library"],
    importpath = "github.com/google/elafros/sample/helloworld",
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
    name = "revisiontemplate",
    images = {
        "sample:latest": ":image",
    },
    template = ":revisiontemplate.yaml",
)

k8s_object(
    name = "updated_revisiontemplate",
    images = {
        "sample:latest": ":image",
    },
    template = ":updated_revisiontemplate.yaml",
)

k8s_object(
    name = "service",
    template = ":elaservice.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        ":service",
        ":revisiontemplate",
    ],
)

k8s_objects(
    name = "updated_everything",
    objects = [
        ":service",
        ":updated_revisiontemplate",
    ],
)
