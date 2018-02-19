load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["async_assertion.go"],
    importpath = "github.com/onsi/gomega/internal/asyncassertion",
    visibility = ["//vendor/github.com/onsi/gomega:__subpackages__"],
    deps = [
        "//vendor/github.com/onsi/gomega/internal/oraclematcher:go_default_library",
        "//vendor/github.com/onsi/gomega/types:go_default_library",
    ],
)

go_test(
    name = "go_default_xtest",
    srcs = [
        "async_assertion_suite_test.go",
        "async_assertion_test.go",
    ],
    importpath = "github.com/onsi/gomega/internal/asyncassertion_test",
    deps = [
        ":go_default_library",
        "//vendor/github.com/onsi/ginkgo:go_default_library",
        "//vendor/github.com/onsi/gomega:go_default_library",
    ],
)
