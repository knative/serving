package(default_visibility = ["//visibility:public"])

exports_files([
    "thumbnailer.yaml",
])

load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "service",
    template = ":thumbnailer.yaml",
)
