load("@k8s_object//:defaults.bzl", "k8s_object")
load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

#
# Common
#
k8s_object(
    name = "common-namespace",
    template = "common/namespace.yaml",
)

k8s_objects(
    name = "common-base",
    objects = [
        ":common-namespace",
    ],
)

#
# Prometheus operator
#
k8s_object(
    name = "prometheus-operator-service-account",
    template = "prometheus-operator/service-account.yaml",
)

k8s_object(
    name = "prometheus-operator-cluster-role",
    template = "prometheus-operator/cluster-role.yaml",
)

k8s_object(
    name = "prometheus-operator-cluster-role-binding",
    template = "prometheus-operator/cluster-role-binding.yaml",
)

k8s_object(
    name = "prometheus-operator-deployment",
    template = "prometheus-operator/deployment.yaml",
)

k8s_object(
    name = "prometheus-operator-service",
    template = "prometheus-operator/service.yaml",
)

k8s_objects(
    name = "prometheus-operator",
    objects = [
        ":prometheus-operator-service-account",
        ":prometheus-operator-cluster-role",
        ":prometheus-operator-cluster-role-binding",
        ":prometheus-operator-deployment",
        ":prometheus-operator-service",
    ],
)

#
# Prometheus
#
k8s_object(
    name = "prometheus-service-account",
    template = "prometheus/service-account.yaml",
)

k8s_object(
    name = "prometheus-roles",
    template = "prometheus/roles.yaml",
)

k8s_object(
    name = "prometheus-role-bindings",
    template = "prometheus/role-bindings.yaml",
)

k8s_object(
    name = "prometheus-prometheus",
    template = "prometheus/prometheus.yaml",
)

k8s_object(
    name = "prometheus-service",
    template = "prometheus/service.yaml",
)

k8s_object(
    name = "prometheus-rules",
    template = "prometheus/rules.yaml",
)

k8s_objects(
    name = "prometheus-base",
    objects = [
        ":prometheus-service-account",
        ":prometheus-roles",
        ":prometheus-role-bindings",
        ":prometheus-service",
        ":prometheus-rules",
    ],
)

#
# Prometheus exporters
#
k8s_object(
    name = "prometheus-exporter-kube-controller-metrics",
    template = "prometheus-exporter/kube-controller-metrics.yaml",
)

k8s_object(
    name = "prometheus-exporter-kube-scheduler-metrics",
    template = "prometheus-exporter/kube-scheduler-metrics.yaml",
)

k8s_object(
    name = "prometheus-exporter-kube-state-metrics",
    template = "prometheus-exporter/kube-state-metrics.yaml",
)

k8s_object(
    name = "prometheus-exporter-node-exporter",
    template = "prometheus-exporter/node-exporter.yaml",
)

k8s_object(
    name = "prometheus-exporter-prometheus",
    template = "prometheus-exporter/prometheus.yaml",
)

k8s_objects(
    name = "prometheus-exporter-common",
    objects = [
        ":prometheus-exporter-node-exporter",
        ":prometheus-exporter-prometheus",
    ],
)

k8s_objects(
    name = "prometheus-exporter-extended",
    objects = [
        ":prometheus-exporter-kube-controller-metrics",
        ":prometheus-exporter-kube-scheduler-metrics",
        ":prometheus-exporter-kube-state-metrics",
    ],
)

#
# Service monitors
#
k8s_object(
    name = "prometheus-service-monitor-apiserver",
    template = "prometheus-servicemonitor/apiserver.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-istio",
    template = "prometheus-servicemonitor/istio.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-kube-controller-manager",
    template = "prometheus-servicemonitor/kube-controller-manager.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-kube-scheduler",
    template = "prometheus-servicemonitor/kube-scheduler.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-kube-state-metrics",
    template = "prometheus-servicemonitor/kube-state-metrics.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-kubelet",
    template = "prometheus-servicemonitor/kubelet.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-node-exporter",
    template = "prometheus-servicemonitor/node-exporter.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-prometheus",
    template = "prometheus-servicemonitor/prometheus.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-prometheus-operator",
    template = "prometheus-servicemonitor/prometheus-operator.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-ela-controller",
    template = "prometheus-servicemonitor/ela-controller.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-fluentd-es",
    template = "prometheus-servicemonitor/fluentd-es.yaml",
)

k8s_objects(
    name = "service-monitor-common",
    objects = [
        ":prometheus-service-monitor-istio",
        ":prometheus-service-monitor-node-exporter",
        ":prometheus-service-monitor-prometheus",
        ":prometheus-service-monitor-prometheus-operator",
        ":prometheus-service-monitor-ela-controller",
        ":prometheus-service-monitor-fluentd-es",
    ],
)

k8s_objects(
    name = "service-monitor-extended",
    objects = [
        ":prometheus-service-monitor-apiserver",
        ":prometheus-service-monitor-kube-controller-manager",
        ":prometheus-service-monitor-kube-scheduler",
        ":prometheus-service-monitor-kube-state-metrics",
        ":prometheus-service-monitor-kubelet",
    ],
)

#
# Grafana
#
k8s_object(
    name = "grafana-deployment",
    template = "grafana/deployment.yaml",
)

k8s_object(
    name = "grafana-service",
    template = "grafana/service.yaml",
)

k8s_object(
    name = "grafana-service-public",
    template = "grafana/service-public.yaml",
)

k8s_object(
    name = "grafana-datasources",
    template = "grafana/datasources.yaml",
)

k8s_object(
    name = "grafana-dashboards",
    template = "grafana/dashboards.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-deployment",
    template = "grafana-dashboard-defs/deployment.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-etcd",
    template = "grafana-dashboard-defs/etcd.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-k8s",
    template = "grafana-dashboard-defs/k8s.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-node",
    template = "grafana-dashboard-defs/node.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-pod",
    template = "grafana-dashboard-defs/pod.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-statefulset",
    template = "grafana-dashboard-defs/statefulset.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-ela-controller",
    template = "grafana-dashboard-defs/ela-controller.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-istio",
    template = "grafana-dashboard-defs/istio.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-istio-mixer",
    template = "grafana-dashboard-defs/istio-mixer.yaml",
)

k8s_object(
    name = "grafana-dashboard-definition-istio-pilot",
    template = "grafana-dashboard-defs/istio-pilot.yaml",
)

k8s_objects(
    name = "grafana",
    objects = [
       ":grafana-datasources",
       ":grafana-dashboards",
       ":grafana-dashboard-definition-node",
       ":grafana-dashboard-definition-pod",
       ":grafana-dashboard-definition-statefulset",
       ":grafana-dashboard-definition-ela-controller",
       ":grafana-dashboard-definition-istio",
       ":grafana-dashboard-definition-istio-mixer",
       ":grafana-dashboard-definition-istio-pilot",
       ":grafana-service",
       ":grafana-deployment",
       ":grafana-dashboard-definition-deployment",
       ":grafana-dashboard-definition-etcd",
       ":grafana-dashboard-definition-k8s",
    ],
)

#
# Fluentd
#
k8s_object(
    name = "fluentd-es-configmap",
    template = "fluentd/fluentd-es-configmap.yaml",
)

k8s_object(
    name = "fluentd-es-configmap-dev",
    template = "fluentd/fluentd-es-configmap-dev.yaml",
)

k8s_object(
    name = "fluentd-es-service",
    template = "fluentd/fluentd-es-service.yaml",
)

k8s_object(
    name = "fluentd-es-istio",
    template = "fluentd/fluentd-es-istio.yaml",
)

k8s_object(
    name = "fluentd-es-ds",
    template = "fluentd/fluentd-es-ds.yaml",
)

k8s_objects(
    name = "fluentd",
    objects = [
        ":fluentd-es-service",
        ":fluentd-es-configmap",
        ":fluentd-es-istio",
        ":fluentd-es-ds",
    ],
)

k8s_objects(
    name = "fluentd-dev",
    objects = [
        ":fluentd-es-service",
        ":fluentd-es-configmap-dev",
        ":fluentd-es-istio",
        ":fluentd-es-ds",
    ],
)

#
# Elastic search Kibana
#
k8s_object(
    name = "es-service",
    template = "elasticsearch/es-service.yaml",
)

k8s_object(
    name = "es-statefulset",
    template = "elasticsearch/es-statefulset.yaml",
)

k8s_objects(
    name = "elasticsearch",
    objects = [
        ":es-service",
        ":es-statefulset",
    ],
)

#
# Kibana
#
k8s_object(
    name = "kibana-deployment",
    template = "kibana/kibana-deployment.yaml",
)

k8s_object(
    name = "kibana-service",
    template = "kibana/kibana-service.yaml",
)

k8s_objects(
    name = "kibana",
    objects = [
        ":kibana-deployment",
        ":kibana-service",
    ],
)

#
# common objects between everything and everything-dev
#
k8s_objects(
    name = "everything-common",
    objects = [
        ":common-base",
        ":prometheus-operator",
        ":prometheus-base",
        ":prometheus-exporter-common",
        ":service-monitor-common",
        ":prometheus-prometheus",
        ":grafana",
        ":elasticsearch",
        ":kibana",
    ],
)

#
# everything: default setup that includes standard logs and metrics
#
k8s_objects(
    name = "everything",
    objects = [
        ":everything-common",
        ":fluentd",
        ":fluentd-es-istio", # For some reason, istio is not applying the rule the first time it was updated. Calling it the second time puts it into effect.
    ],
)

#
# everything-dev: dev setup that includes extra logs and metrics
#
k8s_objects(
    name = "everything-dev",
    objects = [
        ":everything-common",
        ":fluentd",
        ":fluentd-es-istio", # For some reason, istio is not applying the rule the first time it was updated. Calling it the second time puts it into effect.
    ],
)

#
# extended set of metrics
#
k8s_objects(
    name = "metrics-extended",
    objects = [
        ":service-monitor-extended",
        ":prometheus-exporter-extended",
        ":prometheus-service-extended",
        ":grafana-service-public",
    ],
)
