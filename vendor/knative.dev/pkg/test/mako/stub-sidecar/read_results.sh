#!/bin/bash

# Copyright 2019 The Knative Authors
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

# This scripts helps to read results of mako-stub from http

check_command_exists() {
  CMD_NAME=$1
  command -v "$CMD_NAME" > /dev/null || {
    echo "Command $CMD_NAME does not exist"
    exit 1
  }
}

check_command_exists kubectl
check_command_exists curl

if [[ $# -lt 7 ]]
then
  echo "Usage: $0 <mako_stub_pod_name> <mako_stub_namespace> <mako_stub_port> <timeout> <retries> <retries_interval> <out_file>"
  exit 1
fi

MAKO_STUB_POD_NAME="$1"
MAKO_STUB_NAMESPACE="$2"
MAKO_STUB_PORT="$3"
TIMEOUT="$4"
RETRIES="$5"
RETRIES_INTERVAL="$6"
OUTPUT_FILE="$7"

# Find port ready to use

port=10000
isfree=$(netstat -tapln | grep $port)

while [[ -n "$isfree" ]]; do
  port=$((port + 1))
  isfree=$(netstat -tapln | grep $port)
done

for i in $(seq $RETRIES); do
  kubectl port-forward -n "$MAKO_STUB_NAMESPACE" "$MAKO_STUB_POD_NAME" $port:$MAKO_STUB_PORT &
  PORT_FORWARD_PID=$!

  sleep 10

  curl --connect-timeout $TIMEOUT "http://localhost:$port/results" > $OUTPUT_FILE
  curl_exit_status=$?

  kill $PORT_FORWARD_PID
  wait $PORT_FORWARD_PID 2>/dev/null

  if [ 0 -eq $curl_exit_status ]; then
    exit 0
  else
    sleep $RETRIES_INTERVAL
  fi

done

exit 1
