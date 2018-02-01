package(default_visibility = ["//visibility:public"])

exports_files([
    "revisiontemplate.yaml",
    "elaservice.yaml",
])

load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "service",
    template = ":elaservice.yaml",
)
