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

k8s_object(
    name = "istio",
    template = "istio.yaml",
)

k8s_object(
    name = "ds",
    template = "ds.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        ":service",
        ":configmap",
        ":istio",
        ":ds",
        ":istio", # There is a bug where applying this once doesn't take effect and it requires this to be installed one more time to start streaming istio metrics. Will follow with istio team on this one.
    ],
    visibility = ["//visibility:public"],
)

k8s_objects(
    name = "everything-dev",
    objects = [
        ":service",
        ":configmap-dev",
        ":istio",
        ":ds",
        ":istio", # There is a bug where applying this once doesn't take effect and it requires this to be installed one more time to start streaming istio metrics. Will follow with istio team on this one.
    ],
    visibility = ["//visibility:public"],
)
