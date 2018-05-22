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
    name = "deployment",
    template = "deployment-dashboard.json",
)

k8s_object(
    name = "capacity-planning",
    template = "kubernetes-capacity-planning-dashboard.json",
)

k8s_object(
    name = "cluster-health",
    template = "kubernetes-cluster-health-dashboard.json",
)

k8s_object(
    name = "cluster-status",
    template = "kubernetes-cluster-status-dashboard.json",
)

k8s_object(
    name = "control-plane-status",
    template = "kubernetes-control-plane-status-dashboard.json",
)

k8s_object(
    name = "resource-requests",
    template = "kubernetes-resource-requests-dashboard.json",
)

k8s_object(
    name = "nodes",
    template = "nodes-dashboard.json",
)

k8s_object(
    name = "pods",
    template = "pods-dashboard.json",
)

k8s_object(
    name = "statefulset",
    template = "statefulset-dashboard.json",
)

load("@io_bazel_rules_k8s//k8s:objects.bzl", "k8s_objects")

k8s_objects(
    name = "everything",
    objects = [
        "deployment",
        "capacity-planning",
        "cluster-health",
        "cluster-status",
        "control-plane-status",
        "resource-requests",
        "nodes",
        "pods",
        "statefulset",
    ],
    visibility = ["//visibility:public"],
)
