#!/bin/bash
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

mkdir -p ${ARTIFACTS}/dump

while true; do
  date  "+%FT%T.%N" >> ${ARTIFACTS}/dump/config
  kubectl exec $(kubectl -n istio-system get pods -lapp=istio-ingressgateway -oname) -n istio-system -- curl -s localhost:15000/config_dump >> ${ARTIFACTS}/dump/config
  #kubectl exec $(kubectl -n istio-system get pods -lapp=istio-ingressgateway -oname) -n istio-system -- curl -s localhost:15000/config_dump > ${ARTIFACTS}/dump/config-$(date  "+%FT%T.%N")
  date  "+%FT%T.%N" >> ${ARTIFACTS}/dump/clusters
  kubectl exec $(kubectl -n istio-system get pods -lapp=istio-ingressgateway -oname) -n istio-system -- curl -s localhost:15000/clusters >> ${ARTIFACTS}/dump/clusters
  #kubectl exec $(kubectl -n istio-system get pods -lapp=istio-ingressgateway -oname) -n istio-system -- curl -s localhost:15000/clusters > ${ARTIFACTS}/dump/clusters-$(date  "+%FT%T.%N")
#  echo "dump done"
done
