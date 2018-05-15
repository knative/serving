load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_search_default_library",
    srcs = ["search_service.go"],
    importpath = "github.com/elafros/elafros/sample/elafros-routing",
    visibility = ["//visibility:private"],
)

go_binary(
    name = "search_binary",
    embed = [":go_search_default_library"],
    pure = "on",
    visibility = ["//visibility:public"],
)

go_library(
    name = "go_login_default_library",
    srcs = ["login_service.go"],
    importpath = "github.com/elafros/elafros/sample/elafros-routing",
    visibility = ["//visibility:private"],
)

go_binary(
    name = "login_binary",
    embed = [":go_login_default_library"],
    pure = "on",
    visibility = ["//visibility:public"],
)

load("@io_bazel_rules_docker//go:image.bzl", "go_image")

go_image(
    name = "search_image",
    binary = ":search_binary",
)

go_image(
    name = "login_image",
    binary = ":login_binary",
)

load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "search",
    images = {
        "github.com/elafros/elafros/sample/elafros-routing": ":search_image",
    },
    template = ":search.yaml",
)

k8s_object(
    name = "login",
    images = {
        "github.com/elafros/elafros/sample/elafros-routing": ":login_image",
    },
    template = ":login.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        "login",
        "search",
    ],
)
