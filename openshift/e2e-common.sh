#!/usr/bin/env bash

root="$(dirname "${BASH_SOURCE[0]}")"

# shellcheck disable=SC1090
source "$(dirname "$0")/../test/e2e-common.sh"
source "$(dirname "$0")/release/resolve.sh"

readonly SERVING_NAMESPACE=knative-serving
readonly SERVING_INGRESS_NAMESPACE=knative-serving-ingress

# The OLM global namespace was moved to openshift-marketplace since v4.2
# ref: https://jira.coreos.com/browse/OLM-1190
readonly OLM_NAMESPACE="openshift-marketplace"

# Determine if we're running locally or in CI.
if [ -n "$OPENSHIFT_BUILD_NAMESPACE" ]; then
  TEST_IMAGE_TEMPLATE=$(cat <<-END
{{- with .Name }}
{{- if eq . "volumes"}}$KNATIVE_SERVING_TEST_VOLUMES{{end -}}
{{- if eq . "readiness"}}$KNATIVE_SERVING_TEST_READINESS{{end -}}
{{- if eq . "pizzaplanetv1"}}$KNATIVE_SERVING_TEST_PIZZAPLANETV1{{end -}}
{{- if eq . "pizzaplanetv2"}}$KNATIVE_SERVING_TEST_PIZZAPLANETV2{{end -}}
{{- if eq . "helloworld"}}$KNATIVE_SERVING_TEST_HELLOWORLD{{end -}}
{{- if eq . "runtime"}}$KNATIVE_SERVING_TEST_RUNTIME{{end -}}
{{- if eq . "timeout"}}$KNATIVE_SERVING_TEST_TIMEOUT{{end -}}
{{- if eq . "observed-concurrency"}}$KNATIVE_SERVING_TEST_OBSERVED_CONCURRENCY{{end -}}
{{- if eq . "grpc-ping"}}$KNATIVE_SERVING_TEST_GRPC_PING{{end -}}
{{- if eq . "failing"}}$KNATIVE_SERVING_TEST_FAILING{{end -}}
{{- if eq . "autoscale"}}$KNATIVE_SERVING_TEST_AUTOSCALE{{end -}}
{{- if eq . "wsserver"}}$KNATIVE_SERVING_TEST_WSSERVER{{end -}}
{{- if eq . "httpproxy"}}$KNATIVE_SERVING_TEST_HTTPPROXY{{end -}}
{{- if eq . "singlethreaded"}}$KNATIVE_SERVING_TEST_SINGLETHREADED{{end -}}
{{- if eq . "servingcontainer"}}$KNATIVE_SERVING_TEST_SERVINGCONTAINER{{end -}}
{{- if eq . "sidecarcontainer"}}$KNATIVE_SERVING_TEST_SIDECARCONTAINER{{end -}}
{{- if eq . "hellohttp2"}}$KNATIVE_SERVING_TEST_HELLOHTTP2{{end -}}
{{- if eq . "hellovolume"}}$KNATIVE_SERVING_TEST_HELLOVOLUME{{end -}}
{{- if eq . "invalidhelloworld"}}quay.io/openshift-knative/helloworld:invalid{{end -}}
{{end -}}
END
)
elif [ -n "$DOCKER_REPO_OVERRIDE" ]; then
  readonly TEST_IMAGE_TEMPLATE="${DOCKER_REPO_OVERRIDE}/{{.Name}}"
elif [ -n "$BRANCH" ]; then
  readonly TEST_IMAGE_TEMPLATE="registry.ci.openshift.org/openshift/${BRANCH}:knative-serving-test-{{.Name}}"
elif [ -n "$TEMPLATE" ]; then
  readonly TEST_IMAGE_TEMPLATE="$TEMPLATE"
else
  readonly TEST_IMAGE_TEMPLATE="registry.ci.openshift.org/openshift/knative-nightly:knative-serving-test-{{.Name}}"
fi

env

# Waits until the machineset in the given namespaces scales up to the
# desired number of replicas
# Parameters: $1 - namespace
#             $2 - machineset name
#             $3 - desired number of replicas
function wait_until_machineset_scales_up() {
  echo -n "Waiting until machineset $2 in namespace $1 scales up to $3 replicas"
  for _ in {1..150}; do  # timeout after 15 minutes
    local available
    available=$(oc get machineset -n "$1" "$2" -o jsonpath="{.status.availableReplicas}")
    if [[ ${available} -eq $3 ]]; then
      echo -e "\nMachineSet $2 in namespace $1 successfully scaled up to $3 replicas"
      return 0
    fi
    echo -n "."
    sleep 6
  done
  echo - "Error: timeout waiting for machineset $2 in namespace $1 to scale up to $3 replicas"
  return 1
}

# Waits until the given hostname resolves via DNS
# Parameters: $1 - hostname
function wait_until_hostname_resolves() {
  echo -n "Waiting until hostname $1 resolves via DNS"
  for _ in {1..150}; do  # timeout after 15 minutes
    local output
    output=$(host -t a "$1" | grep 'has address')
    if [[ -n "${output}" ]]; then
      echo -e "\n${output}"
      return 0
    fi
    echo -n "."
    sleep 6
  done
  echo -e "\n\nERROR: timeout waiting for hostname $1 to resolve via DNS"
  return 1
}

function serverless_operator_version {

  local branch_name="$(current_branch)"

  if [[ "$branch_name" == "release-v1.14" ]]; then
    echo 'release-1.34'
  elif [[ "$branch_name" == "release-v1.15" ]]; then
    echo 'release-1.35'
  elif [[ "$branch_name" == "release-v1.16" ]]; then
    echo 'release-1.36'
  elif [[ "$branch_name" == "release-v1.17" ]]; then
    echo 'release-1.37'
  elif [[ "$branch_name" == "release-v1.18" ]]; then
    echo 'release-1.38'
  else
    echo 'main'
  fi
}

function install_serverless(){
  header "Installing Serverless Operator"

  # And checkout the setup script based on that commit.
  local SERVERLESS_DIR=$(mktemp -d)

  # Use the absolute path for KNATIVE_SERVING_MANIFESTS_DIR. It is used in `make generated-files`.
  export KNATIVE_SERVING_MANIFESTS_DIR="$(pwd)/openshift/release/artifacts"

  if ! git clone -b $(serverless_operator_version) --depth 1 https://github.com/openshift-knative/serverless-operator.git ${SERVERLESS_DIR}; then
     # As serving branch cuts before SO branch so it fails to clone the branch in the meantime.
     echo "Failed to clone $(serverless_operator_version) SO branch. Use main branch."
     git clone --depth 1 https://github.com/openshift-knative/serverless-operator.git ${SERVERLESS_DIR}
  fi
  pushd ${SERVERLESS_DIR}

  source ./test/lib.bash
  create_namespaces "${SYSTEM_NAMESPACES[@]}"
  export GOPATH=/tmp/go
  export ON_CLUSTER_BUILDS=true
  export DOCKER_REPO_OVERRIDE=image-registry.openshift-image-registry.svc:5000/openshift-marketplace
  #TODO: enable back when we have the feature ready again downstream
  sed -i 's/internal-encryption: "true"/internal-encryption: "false"/g' ./test/v1beta1/resources/operator.knative.dev_v1beta1_knativeserving_cr.yaml
  OPENSHIFT_CI="true" make generated-files images install-serving || return $?

  # Create a secret for https test.
  trust_router_ca || return $?
  popd
}

function install_knative(){
  install_serverless || return $?

  # To enable gRPC and HTTP2 tests without OCP Route.
  oc patch knativeserving knative-serving \
      -n "${SERVING_NAMESPACE}" \
      --type merge --patch '{"spec": {"ingress": {"kourier": {"service-type": "LoadBalancer"}}}}'

  wait_until_service_has_external_ip $SERVING_INGRESS_NAMESPACE kourier || fail_test "Ingress has no external IP"
  wait_until_hostname_resolves "$(kubectl get svc -n $SERVING_INGRESS_NAMESPACE kourier -o jsonpath='{.status.loadBalancer.ingress[0].hostname}')"

  # TODO: Only one cluster enables internal-tls but it should be enabled by default when the feature is stable.
  if [[ ${ENABLE_INTERNAL_TLS:-} == "true" ]]; then
    configure_cm network internal-encryption:true || fail_test
    # As config-kourier is in ingress namespace, don't use configure_cm.
    oc patch knativeserving knative-serving \
        -n "${SERVING_NAMESPACE}" \
        --type merge --patch '{"spec": {"config": {"kourier": {"cluster-cert-secret": "server-certs"}}}}'
    # Deploy certificates for testing TLS with cluster-local gateway
    timeout 600 '[[ $(oc get ns $SERVING_INGRESS_NAMESPACE -oname | wc -l) == 0 ]]' || return 1
    yq read --doc 1 ./test/config/tls/cert-secret.yaml | yq write - metadata.namespace ${SERVING_INGRESS_NAMESPACE} | oc apply -f -
    echo "Restart activator to mount the certificates"
    oc delete pod -n ${SERVING_NAMESPACE} -l app=activator
    oc wait --timeout=60s --for=condition=Available deployment  -n ${SERVING_NAMESPACE} activator
    echo "internal-encryption is enabled"
  else
    # disable internal-encryption. S-O repo would enable by default.
    configure_cm network internal-encryption:false || fail_test
    echo "Restart activator to unmount the certificates"
    oc delete pod -n ${SERVING_NAMESPACE} -l app=activator
    oc wait --timeout=60s --for=condition=Available deployment  -n ${SERVING_NAMESPACE} activator
    echo "internal-encryption is disabled"
  fi

  header "Knative Installed successfully"
}

function prepare_knative_serving_tests_nightly {
  echo ">> Creating test resources for OpenShift (test/config/)"

  kubectl apply -f test/config/cluster-resources.yaml
  kubectl apply -f test/config/test-resources.yaml

  # Apply resource quota in rq-test namespace, needed for the related e2e test.
  oc apply -f ./test/config/resource-quota/resource-quota.yaml

  # Apply persistent volume claim needed, needed for the related e2e test.
  oc apply -f ./test/config/pvc/pvc.yaml

  # Adding scc for anyuid to test TestShouldRunAsUserContainerDefault.
  oc adm policy add-scc-to-user anyuid -z default -n serving-tests

  export SYSTEM_NAMESPACE="$SERVING_NAMESPACE"
  export GATEWAY_OVERRIDE=kourier
  export GATEWAY_NAMESPACE_OVERRIDE="$SERVING_INGRESS_NAMESPACE"
  export INGRESS_CLASS=kourier.ingress.networking.knative.dev

  if [[ ${ENABLE_INTERNAL_TLS} == "true" ]]; then
    # Deploy CA cert for testing TLS with cluster-local gateway
    yq read --doc 0 ./test/config/tls/cert-secret.yaml | oc apply -f -
    # This needs to match the name of Secret in test/config/tls/cert-secret.yaml
    export CA_CERT=ca-cert
    # This needs to match $san from test/config/tls/generate.sh
    export SERVER_NAME=knative.dev
  fi
}

function run_e2e_tests(){
  header "Running tests"

  local test_name=$1
  local failed=0

  # Keep this in sync with test/ha/ha.go
  readonly OPENSHIFT_REPLICAS=2
  # TODO: Increase BUCKETS size more than 1 when operator supports configmap/config-leader-election setting.
  readonly OPENSHIFT_BUCKETS=1

  # Changing the bucket count and cycling the controllers will leave around stale
  # lease resources at the old sharding factor, so clean these up.
  kubectl -n ${SYSTEM_NAMESPACE} delete leases --all

  # Wait for a new leader Controller to prevent race conditions during service reconciliation
  wait_for_leader_controller || failed=1

  # Dump the leases post-setup.
  header "Leaders"
  kubectl get lease -n "${SYSTEM_NAMESPACE}"

  # Give the controller time to sync with the rest of the system components.
  sleep 30
  subdomain=$(oc get ingresses.config.openshift.io cluster  -o jsonpath="{.spec.domain}")

  readonly OPENSHIFT_TEST_OPTIONS="--kubeconfig $KUBECONFIG --enable-beta --enable-alpha --resolvabledomain --customdomain=$subdomain --https --skip-cleanup-on-fail"

  # Enable secure pod defaults for all tests.
  enable_feature_flags secure-pod-defaults || fail_test

  if [ -n "$test_name" ]; then
    go_test_e2e -tags=e2e -timeout=15m -parallel=1 \
    ./test/e2e ./test/conformance/api/... ./test/conformance/runtime/... \
    -run "^(${test_name})$" \
    --imagetemplate "$TEST_IMAGE_TEMPLATE" \
    ${OPENSHIFT_TEST_OPTIONS} || failed=$?
    return $failed
  fi

  local parallel=3

  if [[ $(oc get infrastructure cluster -ojsonpath='{.status.platform}') = VSphere ]]; then
    # Since we don't have LoadBalancers working, gRPC tests will always fail.
    mv ./test/e2e/grpc_test.go /tmp/grpc_test.go
    parallel=2
  fi

  go_test_e2e -tags=e2e -timeout=30m -parallel=$parallel \
    ./test/e2e ./test/conformance/api/... ./test/conformance/runtime/... \
    --imagetemplate "$TEST_IMAGE_TEMPLATE" \
    ${OPENSHIFT_TEST_OPTIONS} || failed=1

 enable_feature_flags tag-header-based-routing || fail_test
 go_test_e2e -timeout=2m ./test/e2e/tagheader \
    --imagetemplate "$TEST_IMAGE_TEMPLATE" \
    ${OPENSHIFT_TEST_OPTIONS} || failed=1
  disable_feature_flags tag-header-based-routing || fail_test

  configure_cm autoscaler allow-zero-initial-scale:true || fail_test
  # wait 10 sec until sync.
  sleep 10
  go_test_e2e -timeout=2m ./test/e2e/initscale \
    --imagetemplate "$TEST_IMAGE_TEMPLATE" \
    ${OPENSHIFT_TEST_OPTIONS} || failed=1
  configure_cm autoscaler allow-zero-initial-scale:false || fail_test

  enable_feature_flags responsive-revision-gc || fail_test
  # immediate_gc
  configure_cm gc retain-since-create-time:disabled retain-since-last-active-time:disabled min-non-active-revisions:0 max-non-active-revisions:0 || fail_test
  go_test_e2e -timeout=2m ./test/e2e/gc \
    --imagetemplate "$TEST_IMAGE_TEMPLATE" \
    ${OPENSHIFT_TEST_OPTIONS} || failed=1
  disable_feature_flags responsive-revision-gc || fail_test

  # Run HPA tests
  go_test_e2e -timeout=30m -tags=hpa ./test/e2e \
    --imagetemplate "$TEST_IMAGE_TEMPLATE" \
    ${OPENSHIFT_TEST_OPTIONS} || failed=1

  # Run init-containers test
  enable_feature_flags kubernetes.podspec-init-containers || fail_test
  go_test_e2e -timeout=2m ./test/e2e/initcontainers \
    --imagetemplate "$TEST_IMAGE_TEMPLATE" \
    ${OPENSHIFT_TEST_OPTIONS} || failed=1
  disable_feature_flags kubernetes.podspec-init-containers || fail_test

  # Run PVC test
  enable_feature_flags kubernetes.podspec-persistent-volume-claim kubernetes.podspec-persistent-volume-write kubernetes.podspec-securitycontext || fail_test
  configure_cm deployment progressDeadline:600s || fail_test
  go_test_e2e -timeout=15m ./test/e2e/pvc \
    --imagetemplate "$TEST_IMAGE_TEMPLATE" \
    ${OPENSHIFT_TEST_OPTIONS} || failed=1
  configure_cm deployment progressDeadline:120s || fail_test
  disable_feature_flags kubernetes.podspec-persistent-volume-claim kubernetes.podspec-persistent-volume-write kubernetes.podspec-securitycontext || fail_test

  enable_feature_flags multi-container-probing || fail_test
  go_test_e2e -timeout=2m ./test/e2e/multicontainerprobing \
    --imagetemplate "$TEST_IMAGE_TEMPLATE" \
    ${OPENSHIFT_TEST_OPTIONS} || failed=$?
  disable_feature_flags multi-container-probing || fail_test

  # Run the helloworld test with an image pulled into the internal registry.
  local image_to_tag=$KNATIVE_SERVING_TEST_HELLOWORLD
  oc tag -n serving-tests "$image_to_tag" "helloworld:latest" --reference-policy=local
  go_test_e2e -tags=e2e -timeout=30m ./test/e2e -run "^(TestHelloWorld)$" \
    ${OPENSHIFT_TEST_OPTIONS} \
    --imagetemplate "image-registry.openshift-image-registry.svc:5000/serving-tests/{{.Name}}" || failed=2

  # Prevent HPA from scaling to make the tests more stable
  oc -n "$SERVING_NAMESPACE" patch hpa activator \
  --type 'merge' \
  --patch '{"spec": {"maxReplicas": '${OPENSHIFT_REPLICAS}', "minReplicas": '${OPENSHIFT_REPLICAS}'}}' || return 1

  # Use sed as the -spoofinterval parameter is not available yet
  sed "s/\(.*requestInterval =\).*/\1 10 * time.Millisecond/" -i vendor/knative.dev/pkg/test/spoof/spoof.go

  # TODO: bring it back once it is stable upstream
  rm -rf test/ha/workload_test.go

  # Run HA tests separately as they're stopping core Knative Serving pods
  # Define short -spoofinterval to ensure frequent probing while stopping pods
  go_test_e2e -tags=e2e -timeout=15m -failfast -parallel=1 \
    ./test/ha \
    -replicas="${OPENSHIFT_REPLICAS}" -buckets="${OPENSHIFT_BUCKETS}" -spoofinterval="10ms" \
    --imagetemplate "$TEST_IMAGE_TEMPLATE" \
    ${OPENSHIFT_TEST_OPTIONS} || failed=1

  # Test gRPC via OpenShift Route.
  # * OCP Route does not work with websocket when enabling default-enable-http2. It will be fixed in the next haproxy version (OCP 4.12 or 4.13).
  # * Also, Skip 4.9, 4.8 job as OCP option for gRPC/HTTP2 is available since 4.10 - bz#1826225
  if [[ ${JOB_NAME} =~ "48" ]] || [[ ${JOB_NAME} =~ "49" ]]; then
        echo "skip gRPC test via OCP"
        return $failed
  fi

  echo "gRPC test via OCP"

  oc annotate ingresses.config/cluster ingress.operator.openshift.io/default-enable-http2=true
  oc annotate knativeserving knative-serving -n knative-serving serverless.openshift.io/default-enable-http2=true

  # This is not necessary actually but it makes sure that access passes through OCP route.
  oc patch knativeserving knative-serving \
      -n "${SERVING_NAMESPACE}" \
      --type merge --patch '{"spec": {"ingress": {"kourier": {"service-type": "ClusterIP"}}}}'

  if [[ $(oc get infrastructure cluster -ojsonpath='{.status.platform}') = VSphere ]]; then
    # Revert grpc_test.go evacuated above.
    mv /tmp/grpc_test.go ./test/e2e/grpc_test.go
    parallel=2
  fi

  # Revert gRPC patch.
  git apply -R ./openshift/patches/004-grpc.patch

  # Run test with the prefix "TestGRPC".
  go_test_e2e -timeout=10m ./test/e2e -parallel=1 \
    -run "TestGRPC" \
    --imagetemplate "$TEST_IMAGE_TEMPLATE" \
    ${OPENSHIFT_TEST_OPTIONS} || failed=1

  # Verify that the right sc is set by default and seccompProfile is injected on OCP >= 4.11.
  go_test_e2e -timeout=10m ./test/e2e/securedefaults -run "^(TestSecureDefaults)$" \
    --imagetemplate "$TEST_IMAGE_TEMPLATE" \
    ${OPENSHIFT_TEST_OPTIONS} || failed=1

  # Allow to use any seccompProfile for non default cases,
  # for more check https://docs.openshift.com/container-platform/4.12/authentication/managing-security-context-constraints.html
  oc adm policy add-scc-to-user privileged -z default -n serving-tests

  # Verify that non secure settings are allowed, although not-recommended.
  # It requires scc privileged or a custom scc that allows any seccompProfile to be set.
  go_test_e2e -timeout=10m ./test/e2e/securedefaults -run "^(TestUnsafePermitted)$" \
    --imagetemplate "$TEST_IMAGE_TEMPLATE" \
    ${OPENSHIFT_TEST_OPTIONS} || failed=1

  return $failed
}

function gather_knative_state {
  logger.info 'Gather knative state'
  local gather_dir="${ARTIFACT_DIR:-/tmp}/gather-knative"
  mkdir -p "$gather_dir"

  oc --insecure-skip-tls-verify adm must-gather \
    --image=quay.io/openshift-knative/must-gather \
    --dest-dir "$gather_dir" > "${gather_dir}/gather-knative.log"
}

function enable_feature_flags {
  local failed=0

  for feature in "$@"; do
    echo "Enabling feature: $feature"
    configure_cm features "$feature":enabled || failed=1
  done
  # Allow settings to be picked up
  sleep 30
  return $failed
}

function disable_feature_flags {
  local failed=0

  for feature in "$@"; do
    echo "Disabling feature: $feature"
    configure_cm features "$feature":disabled || failed=1
  done
  # Allow settings to be picked up
  sleep 30
  return $failed
}
