load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "deployment",
    template = "deployment.yaml",
)

k8s_object(
    name = "etcd",
    template = "etcd.yaml",
)

k8s_object(
    name = "k8s",
    template = "k8s.yaml",
)

k8s_object(
    name = "node",
    template = "node.yaml",
)

k8s_object(
    name = "pod",
    template = "pod.yaml",
)

k8s_object(
    name = "statefulset",
    template = "statefulset.yaml",
)

k8s_object(
    name = "ela-controller",
    template = "ela-controller.yaml",
)

k8s_object(
    name = "istio",
    template = "istio.yaml",
)

k8s_object(
    name = "istio-mixer",
    template = "istio-mixer.yaml",
)

k8s_object(
    name = "istio-pilot",
    template = "istio-pilot.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
       ":node",
       ":pod",
       ":statefulset",
       ":ela-controller",
       ":istio",
       ":istio-mixer",
       ":istio-pilot",
       ":deployment",
       ":etcd",
       ":k8s",
    ],
    visibility = ["//visibility:public"],
)
