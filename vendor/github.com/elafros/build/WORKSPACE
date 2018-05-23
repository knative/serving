workspace(name = "buildcrd")

http_archive(
    name = "io_kubernetes_build",
    sha256 = "cf138e48871629345548b4aaf23101314b5621c1bdbe45c4e75edb45b08891f0",
    strip_prefix = "repo-infra-1fb0a3ff0cc5308a6d8e2f3f9c57d1f2f940354e",
    urls = ["https://github.com/kubernetes/repo-infra/archive/1fb0a3ff0cc5308a6d8e2f3f9c57d1f2f940354e.tar.gz"],
)

# Pull in rules_go
git_repository(
    name = "io_bazel_rules_go",
    # HEAD as of 2018-03-29
    commit = "7de345ea707a8cb29b489f5f4d9a381ba8a98f1a",
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

load(":deps.bzl", "repositories")

repositories()
