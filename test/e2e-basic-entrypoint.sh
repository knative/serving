#!/usr/bin/env bash

# Copyright 2020 The Knative Authors
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

# This script is a workaround supporing running e2e-tests.sh and
# e2e-auto-tls-test.sh for Prow jobs that run both of them as supported before.
# This is introduced as test-infra doesn't support running multiple selected
# test scripts yet. See https://github.com/knative/test-infra/issues/1746

cur_dir="$(dirname ${BASH_SOURCE})"

"${cur_dir}/e2e-tests.sh" $@
"${cur_dir}/e2e-auto-tls-tests.sh" $@
