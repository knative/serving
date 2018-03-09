load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "service",
    template = "service.yaml",
)

k8s_object(
    name = "statefulset",
    template = "statefulset.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        ":service",
        ":statefulset",
    ],
    visibility = ["//visibility:public"],
)
