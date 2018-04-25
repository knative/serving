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
    name = "everything",
    objects = [
        "//third_party/config/monitoring/prometheus-operator/node-exporter:node-exporter",
        ":prometheus",
        ":kube-controller-metrics",
        "//third_party/config/monitoring/kubernetes/kube-state-metrics:everything",
    ],
    visibility = ["//visibility:public"],
)
