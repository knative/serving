load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "istio-logs",
    template = "istio-logs.yaml",
)

k8s_object(
    name = "istio-metrics",
    template = "istio-metrics.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        ":istio-logs",
        ":istio-metrics",
    ],
    visibility = ["//visibility:public"],
)
