load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "fluentd-configmap-dev",
    template = "fluentd-configmap-dev.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        ":fluentd-configmap-dev",
    ],
    visibility = ["//visibility:public"],
)
