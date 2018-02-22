load("@k8s_object//:defaults.bzl", "k8s_object")

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

k8s_object(
    name = "prometheus-exporter-kube-controller-metrics",
    template = "exporter/kube-controller-metrics.yaml",
)

k8s_object(
    name = "prometheus-exporter-kube-scheduler-metrics",
    template = "exporter/kube-scheduler-metrics.yaml",
)

k8s_object(
    name = "prometheus-exporter-kube-state-metrics",
    template = "exporter/kube-state-metrics.yaml",
)

k8s_object(
    name = "prometheus-exporter-node-exporter",
    template = "exporter/node-exporter.yaml",
)

k8s_object(
    name = "prometheus-exporter-prometheus",
    template = "exporter/prometheus.yaml",
)

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
    name = "prometheus-public",
    template = "prometheus/service-public.yaml",
)

k8s_object(
    name = "prometheus-rules",
    template = "prometheus/rules.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-apiserver",
    template = "servicemonitor/apiserver.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-istio",
    template = "servicemonitor/istio.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-kube-controller-manager",
    template = "servicemonitor/kube-controller-manager.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-kube-scheduler",
    template = "servicemonitor/kube-scheduler.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-kube-state-metrics",
    template = "servicemonitor/kube-state-metrics.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-kubelet",
    template = "servicemonitor/kubelet.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-node-exporter",
    template = "servicemonitor/node-exporter.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-prometheus",
    template = "servicemonitor/prometheus.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-prometheus-operator",
    template = "servicemonitor/prometheus-operator.yaml",
)

k8s_object(
    name = "prometheus-service-monitor-ela-controller",
    template = "servicemonitor/ela-controller.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        ":prometheus-operator-service-account",
        ":prometheus-operator-cluster-role",
        ":prometheus-operator-cluster-role-binding",
        ":prometheus-service-account",
        ":prometheus-roles",
        ":prometheus-role-bindings",
        ":prometheus-operator-deployment",
        ":prometheus-operator-service",
        ":prometheus-service",
        ":prometheus-prometheus",
        ":prometheus-rules",
        ":prometheus-exporter-kube-controller-metrics",
        ":prometheus-exporter-kube-scheduler-metrics",
        ":prometheus-exporter-kube-state-metrics",
        ":prometheus-exporter-node-exporter",
        ":prometheus-exporter-prometheus",
        ":prometheus-service-monitor-apiserver",
        ":prometheus-service-monitor-istio",
        ":prometheus-service-monitor-kube-controller-manager",
        ":prometheus-service-monitor-kube-scheduler",
        ":prometheus-service-monitor-kube-state-metrics",
        ":prometheus-service-monitor-kubelet",
        ":prometheus-service-monitor-node-exporter",
        ":prometheus-service-monitor-prometheus",
        ":prometheus-service-monitor-prometheus-operator",
        ":prometheus-service-monitor-ela-controller",
    ],
)

k8s_objects(
    name = "everything-public",
    objects = [
        ":prometheus-public",
    ],
)
