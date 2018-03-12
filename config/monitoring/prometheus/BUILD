load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "service-account",
    template = "service-account.yaml",
)

k8s_object(
    name = "roles",
    template = "roles.yaml",
)

k8s_object(
    name = "role-bindings",
    template = "role-bindings.yaml",
)

k8s_object(
    name = "prometheus-obj",
    template = "prometheus.yaml",
    visibility = ["//visibility:public"],
)

k8s_object(
    name = "service",
    template = "service.yaml",
)

k8s_object(
    name = "rules",
    template = "rules.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything-base",
    objects = [
        ":service-account",
        ":roles",
        ":role-bindings",
        ":service",
        ":rules",
    ],
    visibility = ["//visibility:public"],
)

k8s_objects(
    name = "everything-prometheus",
    objects = [
        ":prometheus-obj",
    ],
    visibility = ["//visibility:public"],
)
