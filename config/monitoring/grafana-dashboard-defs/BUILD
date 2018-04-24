load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "kubernetes",
    template = "kubernetes.yaml",
)

k8s_object(
    name = "elafros",
    template = "elafros.yaml",
)

k8s_object(
    name = "istio",
    template = "istio.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        ":kubernetes",
        ":elafros",
        ":istio",
    ],
    visibility = ["//visibility:public"],
)
