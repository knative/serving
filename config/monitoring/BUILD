load("@k8s_object//:defaults.bzl", "k8s_object")
load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

#
# common objects between everything and everything-dev
#
k8s_objects(
    name = "everything-common",
    objects = [
        "//config/monitoring/common:everything",
        "//config/monitoring/prometheus-operator:everything",
        "//config/monitoring/prometheus:everything-base",
        "//config/monitoring/prometheus-exporter:everything-common",
        "//config/monitoring/prometheus-servicemonitor:everything-common",
        "//config/monitoring/prometheus:everything-prometheus",
        "//config/monitoring/grafana-dashboard-defs:everything",
        "//config/monitoring/grafana:everything",
        "//config/monitoring/elasticsearch:everything",
        "//config/monitoring/kibana:everything",
    ],
)

#
# everything: default setup that includes standard logs and metrics
#
k8s_objects(
    name = "everything",
    objects = [
        ":everything-common",
        "//config/monitoring/fluentd:everything",
    ],
)

#
# everything-dev: dev setup that includes extra logs and metrics
#
k8s_objects(
    name = "everything-dev",
    objects = [
        ":everything-common",
        "//config/monitoring/fluentd:everything-dev",
    ],
)

#
# extended set of metrics
#
k8s_objects(
    name = "metrics-extended",
    objects = [
        "//config/monitoring/prometheus-servicemonitor:everything-extended",
        "//config/monitoring/prometheus-exporter:everything-extended",
    ],
)
