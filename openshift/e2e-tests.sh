#!/usr/bin/env bash

# shellcheck disable=SC1090
source "$(dirname "$0")/e2e-common.sh"

set -x

env

failed=0

export ENABLE_INTERNAL_TLS="${ENABLE_INTERNAL_TLS:-false}"

(( !failed )) && install_knative || failed=1
(( !failed )) && prepare_knative_serving_tests_nightly || failed=2
(( !failed )) && run_e2e_tests || failed=3
(( failed )) && gather_knative_state
(( failed )) && exit $failed

success
