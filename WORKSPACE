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
    commit = "8aeab63328a82fdb8e8eb12f677a4e5ce6b183b1",
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
    commit = "3756369d4920033c32c12d16207e8ee14fee1b18",
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
ISTIO_RELEASE = "0.4.0"
new_http_archive(
    name = "istio_release",
    url = "https://github.com/istio/istio/releases/download/" + ISTIO_RELEASE + "/istio-" + ISTIO_RELEASE + "-linux.tar.gz",
    sha256 = "0085456a6e06afb4366648b507586814be04943ad536756729784f2b0d1ace81",
    type = "tar.gz",
    strip_prefix = "istio-" + ISTIO_RELEASE + "/install/kubernetes",
    build_file_content = "exports_files([\"istio.yaml\"])"
)
