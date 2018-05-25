load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["telemetrysample.go"],
    importpath = "github.com/elafros/elafros/sample/telemetrysample",
    visibility = ["//visibility:private"],
    deps = [
        "//vendor/github.com/openzipkin/zipkin-go:go_default_library",
        "//vendor/github.com/openzipkin/zipkin-go/reporter/http:go_default_library",
        "//vendor/go.opencensus.io/exporter/prometheus:go_default_library",
        "//vendor/go.opencensus.io/exporter/zipkin:go_default_library",
        "//vendor/go.opencensus.io/plugin/ochttp:go_default_library",
        "//vendor/go.opencensus.io/plugin/ochttp/propagation/b3:go_default_library",
        "//vendor/go.opencensus.io/stats:go_default_library",
        "//vendor/go.opencensus.io/stats/view:go_default_library",
        "//vendor/go.opencensus.io/tag:go_default_library",
        "//vendor/go.opencensus.io/trace:go_default_library",
    ],
)

go_binary(
    name = "telemetrysample",
    embed = [":go_default_library"],
    pure = "on",
    visibility = ["//visibility:public"],
)

load("@io_bazel_rules_docker//go:image.bzl", "go_image")

go_image(
    name = "image",
    binary = ":telemetrysample",
)

load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "configuration",
    images = {
        "github.com/elafros/elafros/sample/telemetrysample": ":image",
    },
    template = ":configuration.yaml",
)

k8s_object(
    name = "route",
    template = ":route.yaml",
)

k8s_object(
    name = "prometheus-test",
    template = ":prometheus-test.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        ":route",
        ":configuration",
        ":prometheus-test",
    ],
)
