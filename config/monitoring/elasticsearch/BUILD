load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "service",
    template = "service.yaml",
)

k8s_object(
    name = "serviceaccount",
    template = "serviceaccount.yaml",
)

k8s_object(
    name = "clusterrole",
    template = "clusterrole.yaml",
)

k8s_object(
    name = "clusterrolebinding",
    template = "clusterrolebinding.yaml",
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
        ":serviceaccount",
        ":clusterrole",
        ":clusterrolebinding",
        ":statefulset",
    ],
    visibility = ["//visibility:public"],
)
