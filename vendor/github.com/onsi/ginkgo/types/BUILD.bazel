load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "code_location.go",
        "synchronization.go",
        "types.go",
    ],
    importpath = "github.com/onsi/ginkgo/types",
    visibility = ["//visibility:public"],
)

go_test(
    name = "go_default_xtest",
    srcs = [
        "types_suite_test.go",
        "types_test.go",
    ],
    importpath = "github.com/onsi/ginkgo/types_test",
    deps = [
        ":go_default_library",
        "//vendor/github.com/onsi/ginkgo:go_default_library",
        "//vendor/github.com/onsi/gomega:go_default_library",
    ],
)
