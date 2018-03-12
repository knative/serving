load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "deployment",
    template = "deployment.yaml",
)

k8s_object(
    name = "service",
    template = "service.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        ":deployment",
        ":service",
    ],
    visibility = ["//visibility:public"],
)
