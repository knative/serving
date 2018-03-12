load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "deployment",
    template = "deployment.yaml",
)

k8s_object(
    name = "service",
    template = "service.yaml",
)

k8s_object(
    name = "service-public",
    template = "service-public.yaml",
)

k8s_object(
    name = "datasources",
    template = "datasources.yaml",
)

k8s_object(
    name = "dashboards",
    template = "dashboards.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        ":datasources",
        ":dashboards",
        ":service",
        ":deployment",
    ],
    visibility = ["//visibility:public"],
)
