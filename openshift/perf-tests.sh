#!/usr/bin/env bash

# shellcheck disable=SC1090
source "$(dirname "$0")/e2e-common.sh"

set -x
env

failed=0

git apply ./openshift/performance/patches/*

(( !failed )) && install_knative || failed=1
./openshift/performance/scripts/run-all-performance-tests.sh
(( failed )) && gather_knative_state
(( failed )) && exit $failed

success