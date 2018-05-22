load("@k8s_object//:defaults.bzl", "k8s_object")
load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

#
# everything: default setup that includes standard logs sent to Elasticsearch&Kibana
# and metrics sent to Grafana
#
k8s_objects(
    name = "everything-es",
    objects = [
        "//config/monitoring/100-common:everything",
        "//config/monitoring/150-elasticsearch-prod:everything",
        "//third_party/config/monitoring/common:everything",
        "//third_party/config/monitoring/elasticsearch:everything",
        "//config/monitoring/200-common:everything",
        # Istio has an issue where deploying metric & logging rules once doesn't work and they need
        # to be deployed one more time for istio to pick them up. This second installation is to mitigate
        # the issue
        "//config/monitoring/200-common:istio-only",
    ],
)

#
# everything-es-dev: dev setup that includes extra logs sent to Elasticsearch&Kibana
# and metrics sent to Grafana
#
k8s_objects(
    name = "everything-es-dev",
    objects = [
        "//config/monitoring/100-common:everything",
        "//config/monitoring/150-elasticsearch-dev:everything",
        "//third_party/config/monitoring/common:everything",
        "//third_party/config/monitoring/elasticsearch:everything",
        "//config/monitoring/200-common:everything",
        # Istio has an issue where deploying metric & logging rules once doesn't work and they need
        # to be deployed one more time for istio to pick them up. This second installation is to mitigate
        # the issue
        "//config/monitoring/200-common:istio-only",
    ],
)

#
# everything-sd: standard setup that includes standard logs sent to Stackdriver
# and metrics sent to Grafana
# TODO: Send metrics to Stackdriver
#
k8s_objects(
    name = "everything-sd",
    objects = [
        "//config/monitoring/100-common:everything",
        "//config/monitoring/150-stackdriver-prod:everything",
        "//third_party/config/monitoring/common:everything",
        "//config/monitoring/200-common:everything",
        # Istio has an issue where deploying metric & logging rules once doesn't work and they need
        # to be deployed one more time for istio to pick them up. This second installation is to mitigate
        # the issue
        "//config/monitoring/200-common:istio-only",
    ],
)

#
# everything-sd-dev: dev setup that includes extra logs sent to Stackdriver
# and metrics sent to Grafana
# TODO: Send metrics to Stackdriver
#
k8s_objects(
    name = "everything-sd-dev",
    objects = [
        "//config/monitoring/100-common:everything",
        "//config/monitoring/150-stackdriver-dev:everything",
        "//third_party/config/monitoring/common:everything",
        "//config/monitoring/200-common:everything",
        # Istio has an issue where deploying metric & logging rules once doesn't work and they need
        # to be deployed one more time for istio to pick them up. This second installation is to mitigate
        # the issue
        "//config/monitoring/200-common:istio-only",
    ],
)
