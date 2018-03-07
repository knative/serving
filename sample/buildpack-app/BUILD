load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")
load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "route",
    template = ":route.yaml",
)

# Generate a configuration.yaml based on the DOCKER_REPO_OVERRIDE that folks set.
genrule(
    name = "gen-with-repo",
    srcs = ["configuration.yaml.tpl"],
    outs = ["configuration.yaml"],
    cmd = """
DOCKER_REPO_OVERRIDE=$$(cat bazel-out/stable-status.txt | grep STABLE_DOCKER_REPO | cut -d' ' -f 2)
sed "s@DOCKER_REPO_OVERRIDE@$${DOCKER_REPO_OVERRIDE}@g" < $(location configuration.yaml.tpl) > $(location configuration.yaml)
""",
    stamp = 1,
)

k8s_object(
    name = "configuration",
    template = ":configuration.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        "//sample/templates:buildpack",
        ":route",
        ":configuration",
    ],
)
