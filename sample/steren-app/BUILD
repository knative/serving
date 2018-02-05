load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")
load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "elaservice",
    template = ":elaservice.yaml",
)

# Generate a revisiontemplate.yaml based on the DOCKER_REPO_OVERRIDE that folks set.
genrule(
    name = "gen-with-repo",
    srcs = ["revisiontemplate.yaml.tpl"],
    outs = ["revisiontemplate.yaml"],
    cmd = """
DOCKER_REPO_OVERRIDE=$$(cat bazel-out/stable-status.txt | grep STABLE_DOCKER_REPO | cut -d' ' -f 2)
sed "s@DOCKER_REPO_OVERRIDE@$${DOCKER_REPO_OVERRIDE}@g" < $(location revisiontemplate.yaml.tpl) > $(location revisiontemplate.yaml)
""",
    stamp = 1,
)

k8s_object(
    name = "revisiontemplate",
    template = ":revisiontemplate.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        "//sample/templates:node-app",
        ":elaservice",
        ":revisiontemplate",
    ],
)
