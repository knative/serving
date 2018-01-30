load("@io_bazel_rules_go//go:def.bzl", "gazelle", "go_binary", "go_library", "go_prefix")

go_prefix("github.com/google/elafros")

gazelle(
    name = "gazelle",
    external = "vendored",
)

load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "controller",
    images = {
        "ela-controller:latest": "//cmd/ela-controller:image",
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
    name = "elaservice",
    template = "elaservice.yaml",
)

k8s_object(
    name = "elawebhookservice",
    template = "elawebhookservice.yaml",
)

k8s_object(
    name = "revisiontemplate",
    template = "revisiontemplate.yaml",
)

k8s_object(
    name = "revision",
    template = "revision.yaml",
)

k8s_object(
    name = "istio",
    template = "@istio_release//:istio.yaml",
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
        ":elaservice",
        ":revisiontemplate",
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
        ":istio",  # We depend on Istio.
        # TODO(mattmoor): Add the Build stuff here once we can import it properly.
        ":elafros",
    ],
)
