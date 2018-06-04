# Copyright 2018 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

licenses(["notice"])  # Apache 2

load("@k8s_object//:defaults.bzl", "k8s_object")

k8s_object(
    name = "es-service",
    template = "es-service.yaml",
)

k8s_object(
    name = "es-statefulset",
    template = "es-statefulset.yaml",
)

k8s_object(
    name = "fluentd-es-ds",
    template = "fluentd-es-ds.yaml",
)

k8s_object(
    name = "kibana-deployment",
    template = "kibana-deployment.yaml",
)

k8s_object(
    name = "kibana-service",
    template = "kibana-service.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        "es-service",
        "es-statefulset",
        "kibana-deployment",
        "kibana-service",
        "fluentd-es-ds",
    ],
    visibility = ["//visibility:public"],
)
