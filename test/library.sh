#!/bin/bash

# Copyright 2018 Google, Inc. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This is a collection of useful bash functions and constants, intended
# to be used in test scripts and the like. It doesn't do anything when
# called from command line.

# Default GKE version to be used with Elafros
readonly ELAFROS_GKE_VERSION=1.9.6-gke.1

# Useful environment variables
[[ -n "${PROW_JOB_ID}" ]] && IS_PROW=1 || IS_PROW=0
readonly IS_PROW
readonly ELAFROS_ROOT_DIR="$(dirname $(readlink -f ${BASH_SOURCE}))/.."

# Copy of *_OVERRIDE variables
readonly OG_DOCKER_REPO="${DOCKER_REPO_OVERRIDE}"
readonly OG_K8S_CLUSTER="${K8S_CLUSTER_OVERRIDE}"
readonly OG_K8S_USER="${K8S_USER_OVERRIDE}"

# Returns a UUID
function uuid() {
  # uuidgen is not available in kubekins images
  cat /proc/sys/kernel/random/uuid
}

# Simple header for logging purposes.
function header() {
  echo "================================================="
  echo ${1^^}
  echo "================================================="
}

# Simple subheader for logging purposes.
function subheader() {
  echo "-------------------------------------------------"
  echo $1
  echo "-------------------------------------------------"
}

# Restores the *_OVERRIDE variables to their original value.
function restore_override_vars() {
  export DOCKER_REPO_OVERRIDE="${OG_DOCKER_REPO}"
  export K8S_CLUSTER_OVERRIDE="${OG_K8S_CLUSTER}"
  export K8S_USER_OVERRIDE="${OG_K8S_CLUSTER}"
}

# Remove ALL images in the given GCR repository.
# Parameters: $1 - GCR repository.
function delete_gcr_images() {
  for image in $(gcloud --format='value(name)' container images list --repository=$1); do
    echo "Checking ${image} for removal"
    delete_gcr_images ${image}
    for digest in $(gcloud --format='get(digest)' container images list-tags ${image} --limit=99999); do
      local full_image="${image}@${digest}"
      echo "Removing ${full_image}"
      gcloud container images delete -q --force-delete-tags ${full_image}
    done
  done
}

# Waits until all pods are running in the given namespace.
# Parameters: $1 - namespace.
function wait_until_pods_running() {
  echo -n "Waiting until all pods in namespace $1 are up"
  for i in {1..150}; do  # timeout after 5 minutes
    local pods="$(kubectl get pods -n $1 | grep -v NAME)"
    local not_running=$(echo "${pods}" | grep -v Running | wc -l)
    if [[ -n "${pods}" && ${not_running} == 0 ]]; then
      echo -e "\nAll pods are up:"
      kubectl get pods -n $1
      return 0
    fi
    echo -n "."
    sleep 2
  done
  echo -e "\n\nERROR: timeout waiting for pods to come up"
  kubectl get pods -n $1
  return 1
}

# Returns the name of the Elafros pod of the given app.
# Parameters: $1 - Elafros app name.
function get_ela_pod() {
  kubectl get pods -n ela-system --selector=app=$1 --output=jsonpath="{.items[0].metadata.name}"
}

# Sets the given user as cluster admin.
# Parameters: $1 - user
#             $2 - cluster name
#             $3 - cluster zone
function acquire_cluster_admin_role() {
  # Get the password of the admin and use it, as the service account (or the user)
  # might not have the necessary permission.
  local password=$(gcloud --format="value(masterAuth.password)" \
      container clusters describe $2 --zone=$3)
  kubectl --username=admin --password=$password \
      create clusterrolebinding cluster-admin-binding \
      --clusterrole=cluster-admin \
      --user=$1
}

# Authenticates the current user to GCR in the current project.
function gcr_auth() {
  echo "Authenticating to GCR"
  # kubekins-e2e images lack docker-credential-gcr, install it manually.
  # TODO(adrcunha): Remove this step once docker-credential-gcr is available.
  gcloud components install docker-credential-gcr
  docker-credential-gcr configure-docker
  echo "Successfully authenticated"
}
