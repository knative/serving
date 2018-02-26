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
    name = "grafana-datasources",
    template = "datasources.yaml",
)

k8s_object(
    name = "grafana-dashboards",
    template = "dashboards.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-deployment",
    template = "dashboard-definition/deployment.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-etcd",
    template = "dashboard-definition/etcd.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-k8s",
    template = "dashboard-definition/k8s.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-node",
    template = "dashboard-definition/node.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-pod",
    template = "dashboard-definition/pod.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-statefulset",
    template = "dashboard-definition/statefulset.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-ela-controller",
    template = "dashboard-definition/ela-controller.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-istio",
    template = "dashboard-definition/istio.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-istio-mixer",
    template = "dashboard-definition/istio-mixer.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-istio-pilot",
    template = "dashboard-definition/istio-pilot.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
       ":grafana-datasources",
       ":grafana-dashboards",
       ":grafana-dashboard-definition-deployment",
       ":grafana-dashboard-definition-etcd",
       ":grafana-dashboard-definition-k8s",
       ":grafana-dashboard-definition-node",
       ":grafana-dashboard-definition-pod",
       ":grafana-dashboard-definition-statefulset",
       ":grafana-dashboard-definition-ela-controller",
       ":grafana-dashboard-definition-istio",
       ":grafana-dashboard-definition-istio-mixer",
       ":grafana-dashboard-definition-istio-pilot",
       ":grafana-deployment",
    ],
)

k8s_objects(
    name = "everything-public",
    objects = [
        ":grafana-service-public",
    ],
)
