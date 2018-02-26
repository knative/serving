load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["gitwebhook.go"],
    importpath = "github.com/google/elafros/sample/gitwebhook",
    visibility = ["//visibility:private"],
    deps = [
        "//vendor/github.com/golang/glog:go_default_library",
        "//vendor/github.com/google/go-github/github:go_default_library",
        "//vendor/golang.org/x/oauth2:go_default_library",
        "//vendor/gopkg.in/go-playground/webhooks.v3:go_default_library",
        "//vendor/gopkg.in/go-playground/webhooks.v3/github:go_default_library",
    ],
)

go_binary(
    name = "gitwebhook",
    embed = [":go_default_library"],
    importpath = "github.com/google/elafros/sample/gitwebhook",
    pure = "on",
    visibility = ["//visibility:public"],
)

load("@io_bazel_rules_docker//go:image.bzl", "go_image")

go_image(
    name = "image",
    binary = ":gitwebhook",
)

load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "configuration",
    images = {
        "git-webhook:latest": ":image",
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
