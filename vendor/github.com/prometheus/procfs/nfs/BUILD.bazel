load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "nfs.go",
        "parse.go",
        "parse_nfs.go",
        "parse_nfsd.go",
    ],
    importpath = "github.com/prometheus/procfs/nfs",
    visibility = ["//visibility:public"],
    deps = ["//vendor/github.com/prometheus/procfs/internal/util:go_default_library"],
)

go_test(
    name = "go_default_xtest",
    srcs = [
        "parse_nfs_test.go",
        "parse_nfsd_test.go",
    ],
    deps = [":go_default_library"],
)
