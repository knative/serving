load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["stock.go"],
    importpath = "github.com/elafros/elafros/sample/stock-rest-app",
    visibility = ["//visibility:private"],
    deps = ["//vendor/github.com/gorilla/mux:go_default_library"],
)

go_binary(
    name = "stock-rest-app",
    embed = [":go_default_library"],
    visibility = ["//visibility:public"],
)

load("@io_bazel_rules_docker//go:image.bzl", "go_image")

go_image(
    name = "image",
    binary = ":stock-rest-app",
)

load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "everything",
    images = {
        "github.com/elafros/elafros/sample/stock-rest-app": ":image",
    },
    template = ":sample.yaml",
)

k8s_object(
    name = "updated_configuration",
    images = {
        "github.com/elafros/elafros/sample/stock-rest-app": ":image",
    },
    template = ":updated_configuration.yaml",
)
