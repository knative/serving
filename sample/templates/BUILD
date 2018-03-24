package(default_visibility = ["//visibility:public"])

load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "docker-build",
    template = "docker-build.yaml",
)

k8s_object(
    name = "docker-build-helper",
    template = "docker-build-helper.yaml",
)

k8s_object(
    name = "node-app",
    template = "node-app.yaml",
)

k8s_object(
    name = "node-fn",
    template = "node-fn.yaml",
)

k8s_object(
    name = "buildpack",
    template = "buildpack.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "all",
    objects = [
        ":docker-build",
        ":docker-build-helper",
        ":node-app",
        ":node-fn",
        ":buildpack",
    ],
)
