load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "namespace",
    template = "100-namespace.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        ":namespace",
    ],
    visibility = ["//visibility:public"],
)
