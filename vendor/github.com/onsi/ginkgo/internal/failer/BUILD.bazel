load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["failer.go"],
    importpath = "github.com/onsi/ginkgo/internal/failer",
    visibility = ["//vendor/github.com/onsi/ginkgo:__subpackages__"],
    deps = ["//vendor/github.com/onsi/ginkgo/types:go_default_library"],
)

go_test(
    name = "go_default_xtest",
    srcs = [
        "failer_suite_test.go",
        "failer_test.go",
    ],
    importpath = "github.com/onsi/ginkgo/internal/failer_test",
    deps = [
        ":go_default_library",
        "//vendor/github.com/onsi/ginkgo:go_default_library",
        "//vendor/github.com/onsi/ginkgo/internal/codelocation:go_default_library",
        "//vendor/github.com/onsi/ginkgo/types:go_default_library",
        "//vendor/github.com/onsi/gomega:go_default_library",
    ],
)
