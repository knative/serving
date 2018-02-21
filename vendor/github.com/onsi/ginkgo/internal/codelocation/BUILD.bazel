load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = ["code_location.go"],
    importpath = "github.com/onsi/ginkgo/internal/codelocation",
    visibility = ["//vendor/github.com/onsi/ginkgo:__subpackages__"],
    deps = ["//vendor/github.com/onsi/ginkgo/types:go_default_library"],
)

go_test(
    name = "go_default_xtest",
    srcs = [
        "code_location_suite_test.go",
        "code_location_test.go",
    ],
    importpath = "github.com/onsi/ginkgo/internal/codelocation_test",
    deps = [
        ":go_default_library",
        "//vendor/github.com/onsi/ginkgo:go_default_library",
        "//vendor/github.com/onsi/ginkgo/types:go_default_library",
        "//vendor/github.com/onsi/gomega:go_default_library",
    ],
)
