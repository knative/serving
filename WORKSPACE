workspace(name = "elafros")

http_archive(
    name = "io_kubernetes_build",
    sha256 = "cf138e48871629345548b4aaf23101314b5621c1bdbe45c4e75edb45b08891f0",
    strip_prefix = "repo-infra-1fb0a3ff0cc5308a6d8e2f3f9c57d1f2f940354e",
    urls = ["https://github.com/kubernetes/repo-infra/archive/1fb0a3ff0cc5308a6d8e2f3f9c57d1f2f940354e.tar.gz"],
)

# Pull in rules_go
git_repository(
    name = "io_bazel_rules_go",
    commit = "737df20c53499fd84b67f04c6ca9ccdee2e77089",
    remote = "https://github.com/bazelbuild/rules_go.git",
)

load("@io_bazel_rules_go//go:def.bzl", "go_rules_dependencies", "go_register_toolchains", "go_repository")

go_rules_dependencies()

go_register_toolchains()

# Pull in rules_docker
git_repository(
    name = "io_bazel_rules_docker",
    # HEAD as of 2018-02-13
    commit = "c7f9eaa63bc3a31acab5e399c72b4e5228ab5ad7",
    remote = "https://github.com/bazelbuild/rules_docker.git",
)

load(
    "@io_bazel_rules_docker//docker:docker.bzl",
    "docker_repositories",
)

docker_repositories()

# Pull in the go_image stuff.
load(
    "@io_bazel_rules_docker//go:image.bzl",
    _go_image_repos = "repositories",
)

_go_image_repos()

# Pull in rules_k8s
git_repository(
    name = "io_bazel_rules_k8s",
    # HEAD as of 2018-02-09
    commit = "761ba3bdf864f85cae7d3e65b045197ad208e1a0",
    remote = "https://github.com/bazelbuild/rules_k8s",
)

load("@io_bazel_rules_k8s//k8s:k8s.bzl", "k8s_repositories", "k8s_defaults")

k8s_repositories()

# See ./print-workspace-status.sh for definitions.
_CLUSTER = "{STABLE_K8S_CLUSTER}"

_REPOSITORY = "{STABLE_DOCKER_REPO}"

k8s_defaults(
    name = "k8s_object",
    cluster = _CLUSTER,
    image_chroot = _REPOSITORY,
)

go_repository(
    name = "io_k8s_code_generator",
    commit = "3c1fe2637f4efce271f1e6f50e039b2a0467c60c",
    importpath = "k8s.io/code-generator",
)

go_repository(
    name = "io_k8s_gengo",
    commit = "1ef560bbde5195c01629039ad3b337ce63e7b321",
    importpath = "k8s.io/gengo",
)

go_repository(
    name = "com_github_spf13_pflag",
    commit = "4c012f6dcd9546820e378d0bdda4d8fc772cdfea",
    importpath = "github.com/spf13/pflag",
)

# Istio
ISTIO_RELEASE = "0.5.1"

new_http_archive(
    name = "istio_release",
    build_file = "BUILD.istio",
    sha256 = "f811d29df1b14f71f9e4c14622c949f8b7a1a049ca970e62811933e37a128727",
    strip_prefix = "istio-" + ISTIO_RELEASE + "/install/kubernetes",
    type = "tar.gz",
    url = "https://github.com/istio/istio/releases/download/" + ISTIO_RELEASE + "/istio-" + ISTIO_RELEASE + "-linux.tar.gz",
)

# Until the Build repo is public, we must use the Skylark-based git_repository rules
# per the documentation: https://docs.bazel.build/versions/master/be/workspace.html#git_repository
load(
    "@bazel_tools//tools/build_defs/repo:git.bzl",
    private_git_repository = "git_repository",
)

private_git_repository(
    name = "buildcrd",
    commit = "b8a41a0ce5e77c6269c433c9e50ca8b2a3d69547",
    remote = "git@github.com:elafros/build-crd.git",
)

# If you would like to test changes to both repositories,
# you can comment the above and uncomment this:
# local_repository(
#    name = "buildcrd",
#    path = "../build-crd",
# )

load("@buildcrd//:deps.bzl", "repositories")

repositories()
