load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "service-account",
    template = "service-account.yaml",
)

k8s_object(
    name = "cluster-role",
    template = "cluster-role.yaml",
)

k8s_object(
    name = "cluster-role-binding",
    template = "cluster-role-binding.yaml",
)

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
        ":service-account",
        ":cluster-role",
        ":cluster-role-binding",
        ":deployment",
        ":service",
    ],
    visibility = ["//visibility:public"],
)
