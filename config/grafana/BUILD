load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "grafana-deployment",
    template = "deployment.yaml",
)

k8s_object(
    name = "grafana-service-public",
    template = "service-public.yaml",
)

k8s_object(
    name = "grafana-dashboards",
    template = "dashboards.yaml",
)

k8s_object(
    name = "grafana-dashboard-definitions",
    template = "dashboard-definitions.yaml",
)

k8s_object(
    name = "grafana-datasources",
    template = "datasources.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
       ":grafana-datasources",
       ":grafana-dashboards",
       ":grafana-dashboard-definitions",
       ":grafana-deployment",
    ],
)

k8s_objects(
    name = "everything-public",
    objects = [
        ":grafana-service-public",
    ],
)
