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
    importpath = "github.com/elafros/elafros/pkg/apis/cloudbuild/v1alpha1",
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/apis/cloudbuild:go_default_library",
        "//vendor/k8s.io/api/core/v1:go_default_library",
        "//vendor/k8s.io/apimachinery/pkg/apis/meta/v1:go_default_library",
        "//vendor/k8s.io/apimachinery/pkg/runtime:go_default_library",
        "//vendor/k8s.io/apimachinery/pkg/runtime/schema:go_default_library",
    ],
)

# This test verifies that our checked in type definitions match
# the version of the CRD we are linking against in //:everything
sh_test(
    name = "pristine_test",
    srcs = ["pristine_test.sh"],
    data = [
        ":build_template_types.go",
        ":build_types.go",
        ":doc.go",
        "@buildcrd//pkg/apis/cloudbuild/v1alpha1:build_template_types.go",
        "@buildcrd//pkg/apis/cloudbuild/v1alpha1:build_types.go",
        "@buildcrd//pkg/apis/cloudbuild/v1alpha1:doc.go",
    ],
)
