load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "fake_writer.go",
        "writer.go",
    ],
    importpath = "github.com/onsi/ginkgo/internal/writer",
    visibility = ["//vendor/github.com/onsi/ginkgo:__subpackages__"],
)

go_test(
    name = "go_default_xtest",
    srcs = [
        "writer_suite_test.go",
        "writer_test.go",
    ],
    importpath = "github.com/onsi/ginkgo/internal/writer_test",
    deps = [
        ":go_default_library",
        "//vendor/github.com/onsi/ginkgo:go_default_library",
        "//vendor/github.com/onsi/gomega:go_default_library",
        "//vendor/github.com/onsi/gomega/gbytes:go_default_library",
    ],
)
