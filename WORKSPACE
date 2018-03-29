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
    # HEAD as of 2018-03-09
    commit = "483759bba7be220a1014e7ba1cf989f052fefa2c",
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
    # HEAD as of 2018-03-09
    commit = "4348f8e28b70cf3aff7ca8e008e8dc7ac49bad92",
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

# We rewrite things in ./hack/update-deps.sh to use this version.
go_repository(
    name = "io_k8s_code_generator",
    tag = "kubernetes-1.10.0",
    importpath = "k8s.io/code-generator",
)

go_repository(
    name = "io_k8s_gengo",
    commit = "01a732e01d00cb9a81bb0ca050d3e6d2b947927b",
    importpath = "k8s.io/gengo",
)

go_repository(
    name = "com_github_spf13_pflag",
    commit = "4c012f6dcd9546820e378d0bdda4d8fc772cdfea",
    importpath = "github.com/spf13/pflag",
)

# Istio
ISTIO_RELEASE = "0.6.0"

new_http_archive(
    name = "istio_release",
    build_file = "BUILD.istio",
    sha256 = "fa9bc2c6a197096812b6f4a5a284d13b38bbdba4ee1fc6586a60c9a63337b4d8",
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
    commit = "098d36d81cf77ffd77d3743f514f0425b29c9597",
    remote = "git@github.com:elafros/build.git",
)

# If you would like to test changes to both repositories,
# you can comment the above and uncomment this:
# local_repository(
#    name = "buildcrd",
#    path = "../build",
# )

load("@buildcrd//:deps.bzl", "repositories")

repositories()

load(":ca_bundle.bzl", "cluster_ca_bundle")

cluster_ca_bundle(name = "cluster_ca_bundle")
