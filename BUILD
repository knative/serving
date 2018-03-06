load("@io_bazel_rules_go//go:def.bzl", "gazelle", "go_binary", "go_library", "go_prefix")

go_prefix("github.com/elafros/elafros")

gazelle(
    name = "gazelle",
    external = "vendored",
)

load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "controller",
    images = {
        "ela-controller:latest": "//cmd/ela-controller:image",
        "ela-queue:latest": "//cmd/ela-queue:image",
        "ela-autoscaler:latest": "//cmd/ela-autoscaler:image",
    },
    template = "controller.yaml",
)

k8s_object(
    name = "webhook",
    images = {
        "ela-webhook:latest": "//cmd/ela-webhook:image",
    },
    template = "webhook.yaml",
)

k8s_object(
    name = "namespace",
    template = "namespace.yaml",
)

k8s_object(
    name = "serviceaccount",
    template = "serviceaccount.yaml",
)

k8s_object(
    name = "clusterrolebinding",
    template = "clusterrolebinding.yaml",
)

k8s_object(
    name = "route",
    template = "route.yaml",
)

k8s_object(
    name = "elawebhookservice",
    template = "elawebhookservice.yaml",
)

k8s_object(
    name = "configuration",
    template = "configuration.yaml",
)

k8s_object(
    name = "revision",
    template = "revision.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "authz",
    objects = [
        ":serviceaccount",
        ":clusterrolebinding",
    ],
)

k8s_objects(
    name = "crds",
    objects = [
        ":route",
        ":configuration",
        ":revision",
    ],
)

# All of our stuff goes here.
k8s_objects(
    name = "elafros",
    objects = [
        ":namespace",
        ":authz",
        ":crds",
        ":controller",
        ":webhook",
        ":elawebhookservice",
    ],
)

k8s_objects(
    name = "everything",
    objects = [
        "@istio_release//:istio",  # We depend on Istio.
        "@buildcrd//:everything",
        ":elafros",
    ],
)
