load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "namespace",
    template = "namespace.yaml",
    visibility = ["//visibility:public"],
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

#
# common objects between everything and everything-dev
#
k8s_objects(
    name = "everything-common",
    objects = [
        ":namespace",
        "//config/monitoring/prometheus-operator:everything",
        "//config/monitoring/prometheus:everything-base",
        "//config/monitoring/prometheus-exporter:everything-common",
        "//config/monitoring/prometheus-servicemonitor:everything-common",
        "//config/monitoring/prometheus:everything-prometheus",
        "//config/monitoring/grafana-dashboard-defs:everything",
        "//config/monitoring/grafana:everything",
        "//config/monitoring/elasticsearch:everything",
        "//config/monitoring/kibana:everything",
        "//config/monitoring/zipkin:everything",
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
        # Istio has an issue where deploying metric & logging rules once doesn't work and they need 
        # to be deployed one more time for istio to pick them up. This second installation is to mitigate
        # the issue
        "//config/monitoring/fluentd:istio",
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
        # Istio has an issue where deploying metric & logging rules once doesn't work and they need 
        # to be deployed one more time for istio to pick them up. This second installation is to mitigate
        # the issue
        "//config/monitoring/fluentd:istio",
    ],
)

#
# Extended set of k8s metrics
#
k8s_objects(
    name = "k8s-metrics",
    objects = [
        "//config/monitoring/prometheus-servicemonitor:k8s-metrics",
        "//config/monitoring/prometheus-exporter:k8s-metrics",
    ],
)
