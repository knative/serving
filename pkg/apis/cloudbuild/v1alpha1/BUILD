load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "build_template_types.go",
        "build_types.go",
        "doc.go",
        "register.go",
        "zz_generated.deepcopy.go",
    ],
    importpath = "github.com/google/elafros/pkg/apis/cloudbuild/v1alpha1",
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/apis/cloudbuild:go_default_library",
        "//vendor/k8s.io/api/core/v1:go_default_library",
        "//vendor/k8s.io/apimachinery/pkg/apis/meta/v1:go_default_library",
        "//vendor/k8s.io/apimachinery/pkg/runtime:go_default_library",
        "//vendor/k8s.io/apimachinery/pkg/runtime/schema:go_default_library",
    ],
)
