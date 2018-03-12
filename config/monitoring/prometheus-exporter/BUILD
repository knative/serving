load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "kube-controller-metrics",
    template = "kube-controller-metrics.yaml",
)

k8s_object(
    name = "kube-state-metrics",
    template = "kube-state-metrics.yaml",
)

k8s_object(
    name = "node-exporter",
    template = "node-exporter.yaml",
)

k8s_object(
    name = "prometheus",
    template = "prometheus.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything-common",
    objects = [
        ":node-exporter",
        ":prometheus",
    ],
    visibility = ["//visibility:public"],
)

#
# Enable detailed k8s metric collection. 
# Below is not enabled by default as it installs a large set of k8s metrics.
#
k8s_objects(
    name = "k8s-metrics",
    objects = [
        ":kube-controller-metrics",
        ":kube-state-metrics",
    ],
    visibility = ["//visibility:public"],
)
