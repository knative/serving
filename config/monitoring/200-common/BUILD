load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "fluentd",
    template = "100-fluentd.yaml",
)

k8s_object(
    name = "grafana",
    template = "100-grafana.yaml",
)

k8s_object(
    name = "zipkin",
    template = "100-zipkin.yaml",
)

k8s_object(
    name = "istio",
    template = "100-istio.yaml",
)

k8s_object(
    name = "grafana-dash-knative",
    template = "100-grafana-dash-knative.yaml",
)

k8s_object(
    name = "grafana-dash-knative-efficiency",
    template = "100-grafana-dash-knative-efficiency.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        ":fluentd",
        ":grafana",
        ":zipkin",
        ":istio",
        ":grafana-dash-knative",
        ":grafana-dash-knative-efficiency",
        "//config/monitoring/200-common/200-prometheus-exporter:everything",
        "//config/monitoring/200-common/300-prometheus:everything",
    ],
    visibility = ["//visibility:public"],
)

k8s_objects(
    name = "istio-only",
    objects = [
        ":istio",
    ],
    visibility = ["//visibility:public"],
)
