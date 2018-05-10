load("@k8s_object//:defaults.bzl", "k8s_object")
load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything-common",
    objects = [
        "//config/monitoring/100-common:everything",
        "//third_party/config/monitoring:everything",
        "//config/monitoring/200-common:everything",
    ],
)

#
# everything: default setup that includes standard logs and metrics
#
k8s_objects(
    name = "everything",
    objects = [
        ":everything-common",
        "//config/monitoring/300-prod:everything",
        # Istio has an issue where deploying metric & logging rules once doesn't work and they need
        # to be deployed one more time for istio to pick them up. This second installation is to mitigate
        # the issue
        "//config/monitoring/200-common:istio-only",
    ],
)

#
# everything-dev: dev setup that includes extra logs and metrics
#
k8s_objects(
    name = "everything-dev",
    objects = [
        ":everything-common",
        "//config/monitoring/300-dev:everything",
        # Istio has an issue where deploying metric & logging rules once doesn't work and they need
        # to be deployed one more time for istio to pick them up. This second installation is to mitigate
        # the issue
        "//config/monitoring/200-common:istio-only",
    ],
)
