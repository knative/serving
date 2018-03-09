load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "apiserver",
    template = "apiserver.yaml",
)

k8s_object(
    name = "istio",
    template = "istio.yaml",
)

k8s_object(
    name = "kube-controller-manager",
    template = "kube-controller-manager.yaml",
)

k8s_object(
    name = "kube-scheduler",
    template = "kube-scheduler.yaml",
)

k8s_object(
    name = "kube-state-metrics",
    template = "kube-state-metrics.yaml",
)

k8s_object(
    name = "kubelet",
    template = "kubelet.yaml",
)

k8s_object(
    name = "node-exporter",
    template = "node-exporter.yaml",
)

k8s_object(
    name = "prometheus",
    template = "prometheus.yaml",
)

k8s_object(
    name = "prometheus-operator",
    template = "prometheus-operator.yaml",
)

k8s_object(
    name = "ela-controller",
    template = "ela-controller.yaml",
)

k8s_object(
    name = "fluentd-es",
    template = "fluentd-es.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything-common",
    objects = [
        ":istio",
        ":node-exporter",
        ":prometheus",
        ":prometheus-operator",
        ":ela-controller",
        ":fluentd-es",
    ],
    visibility = ["//visibility:public"],
)

k8s_objects(
    name = "everything-extended",
    objects = [
        ":apiserver",
        ":kube-controller-manager",
        ":kube-scheduler",
        ":kube-state-metrics",
        ":kubelet",
    ],
    visibility = ["//visibility:public"],
)
