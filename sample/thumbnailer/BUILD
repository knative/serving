package(default_visibility = ["//visibility:public"])

exports_files([
    "thumbnailer.yaml",
])

load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "everything",
    template = ":thumbnailer.yaml",
)
