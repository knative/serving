load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "scrape-config",
    template = "100-scrape-config.yaml",
)

k8s_object(
    name = "prometheus",
    template = "200-prometheus.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        ":scrape-config",
        ":prometheus",
    ],
    visibility = ["//visibility:public"],
)
