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
    name = "istio-rules",
    template = "istio.yaml",
)

k8s_object(
    name = "ds",
    template = "ds.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "istio",
    objects = [
        ":istio-rules",
    ],
    visibility = ["//visibility:public"],
)

k8s_objects(
    name = "everything",
    objects = [
        ":service",
        ":configmap",
        ":istio-rules",
        ":ds",
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
    ],
    visibility = ["//visibility:public"],
)
