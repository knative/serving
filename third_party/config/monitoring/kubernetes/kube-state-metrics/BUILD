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
    name = "cluster-role-binding",
    template = "kube-state-metrics-cluster-role-binding.yaml",
)

k8s_object(
    name = "cluster-role",
    template = "kube-state-metrics-cluster-role.yaml",
)

k8s_object(
    name = "deployment",
    template = "kube-state-metrics-deployment.yaml",
)

k8s_object(
    name = "role-binding",
    template = "kube-state-metrics-role-binding.yaml",
)

k8s_object(
    name = "role",
    template = "kube-state-metrics-role.yaml",
)

k8s_object(
    name = "service-account",
    template = "kube-state-metrics-service-account.yaml",
)

k8s_object(
    name = "service",
    template = "kube-state-metrics-service.yaml",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        "service-account",
        "role",
        "role-binding",
        "cluster-role",
        "cluster-role-binding",
        "deployment",
        "service",
    ],
    visibility = ["//visibility:public"],
)
