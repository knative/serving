load("@python_helloworld_pip//:requirements.bzl", "all_requirements")
load('@io_bazel_rules_docker//python:image.bzl', 'py_image')

py_image(
    name = "image",
    srcs = ["main.py"],
    main = "main.py",
    deps = all_requirements,
)

load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "configuration",
    images = {
        "python-sample:latest": ":image",
    },
    template = ":configuration.yaml",
)

k8s_object(
    name = "route",
    template = ":route.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        ":route",
        ":configuration",
    ],
)
