load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "configmap",
    template = "configmap.yaml",
)

k8s_object(
    name = "configmap-dev",
    template = "configmap-dev.yaml",
)

k8s_object(
    name = "service",
    template = "service.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        ":service",
        ":configmap",
        "//third_party/config/monitoring/kubernetes/elasticsearch:fluentd",
    ],
    visibility = ["//visibility:public"],
)

k8s_objects(
    name = "everything-dev",
    objects = [
        ":service",
        ":configmap-dev",
        "//third_party/config/monitoring/kubernetes/elasticsearch:fluentd",
    ],
    visibility = ["//visibility:public"],
)
