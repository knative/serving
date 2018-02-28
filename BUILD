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

# Generate a istioclusterrolebinding.yaml based on the
# K8S_USER_OVERRIDE env variable.
genrule(
    name = "gen-istioclusterrolebinding",
    srcs = ["istioclusterrolebinding.yaml.tpl"],
    outs = ["istioclusterrolebinding.yaml"],
    cmd = """
K8S_USER_OVERRIDE=$$(grep STABLE_K8S_USER bazel-out/stable-status.txt | cut -d' ' -f 2)
sed "s/K8S_USER_OVERRIDE/$${K8S_USER_OVERRIDE}/g" $(location istioclusterrolebinding.yaml.tpl) > $(location istioclusterrolebinding.yaml)
""",
    stamp = 1,
)

k8s_object(
    name = "istioclusterrolebinding",
    template = "istioclusterrolebinding.yaml",
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
        ":istioclusterrolebinding",
        "@istio_release//:istio",  # We depend on Istio.
        "@buildcrd//:everything",
        ":elafros",
    ],
)
