#!/usr/bin/env bash

set -x
declare SERVING
if [[ -z "${SERVING}" ]]; then
  SERVING="$(dirname "${BASH_SOURCE[0]}")/../../.."
fi

set -o errexit
set -o pipefail

# shellcheck disable=SC1090
# shellcheck disable=SC1091
source "${SERVING}/test/e2e-common.sh"

declare JOB_NAME
declare BUILD_ID
declare ARTIFACTS
# shellcheck disable=SC2034
declare AUTH
declare ES_DEVELOPMENT

ns="default"

function run_job() {
  local name=$1
  local file=$2
  local REPO=$3

  TEST_IMAGE_TEMPLATE=$(cat <<-END
{{- with .Name }}
{{- if eq . "helloworld"}}$KNATIVE_SERVING_TEST_HELLOWORLD{{end -}}
{{- if eq . "slowstart"}}$KNATIVE_SERVING_TEST_SLOWSTART{{end -}}
{{- if eq . "runtime"}}$KNATIVE_SERVING_TEST_RUNTIME{{end -}}
{{end -}}
END
)

# shellcheck disable=SC2086
  TEST_IMAGE_TEMPLATE=$(echo ${TEST_IMAGE_TEMPLATE} | sed -e 's/\\/\\\\/g; s/\//\\\//g; s/&/\\\&/g')

  # cleanup from old runs
  oc delete job "$name" -n "$ns" --ignore-not-found=true

  # start the load test and get the logs
  pushd "$SERVING"
  sed "s|\$SYSTEM_NAMESPACE|$SYSTEM_NAMESPACE|g" "$file" | sed "s|\$IMAGE_TEMPLATE_REPLACE|-imagetemplate=$TEST_IMAGE_TEMPLATE|g" | sed "s|\$KO_DOCKER_REPO|$REPO|g" | sed "s|\$USE_OPEN_SEARCH|\"$USE_OPEN_SEARCH\"|g" | sed "s|\$USE_ES|'false'|g" | oc apply -f -
  popd

  sleep 5

  # Follow logs to wait for job termination
  oc wait --for=condition=ready -n "$ns" pod --selector=job-name="$name" --timeout=-1s
  oc logs -n "$ns" -f "job.batch/$name"

  # Dump logs to a file to upload it as CI job artifact
  oc logs -n "$ns" "job.batch/$name" >"$ARTIFACTS/$name.log"

  # clean up
  oc delete "job/$name" -n "$ns" --ignore-not-found=true
  oc wait --for=delete "job/$name" --timeout=60s -n "$ns"
}

# If ES_DEVELOPMENT is specified we run against a non-secured development instance
# with no authentication
if [[ "${ES_DEVELOPMENT:-false}" == "true" ]]; then
  export ES_URL="https://${ES_HOST_PORT}"
else
  if [[ -z "${ES_USERNAME}" ]]; then
    ES_USERNAME=$(cat "/secret/username")
  fi

  if [[ -z "${ES_PASSWORD}" ]]; then
    ES_PASSWORD=$(cat "/secret/password")
  fi

  if [[ -z "${ES_HOST_PORT}" ]]; then
    ES_HOST_PORT="search-ocp-qe-perf-scale-test-elk-hcm7wtsqpxy7xogbu72bor4uve.us-east-1.es.amazonaws.com"
  fi
  export ES_URL="https://${ES_USERNAME}:${ES_PASSWORD}@${ES_HOST_PORT}"
fi

if [[ -z "${JOB_NAME}" ]]; then
  JOB_NAME="local"
fi

if [[ -z "${BUILD_ID}" ]]; then
  BUILD_ID="local"
fi

echo "Running load test with BUILD_ID: ${BUILD_ID}, JOB_NAME: ${JOB_NAME}, reporting results to: ${ES_HOST_PORT}"

###############################################################################################
header "Preparing cluster config"

kubectl delete secret performance-test-config -n "$ns" --ignore-not-found=true

kubectl create secret generic performance-test-config -n "$ns" \
  --from-literal=esurl="${ES_URL}" \
  --from-literal=jobname="${JOB_NAME}" \
  --from-literal=buildid="${BUILD_ID}"
