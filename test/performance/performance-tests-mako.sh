#!/usr/bin/env bash

# Copyright 2022 The Knative Authors
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

# This script runs the performance tests using mako sidecar stub against Knative
# Serving built from source. It can be optionally started for each PR.
# For convenience, it can also be executed manually.

# If you already have a Kubernetes cluster setup and kubectl pointing
# to it, call this script with the --run-tests arguments and it will use
# the cluster and run the tests.

# Calling this script without arguments will create a new cluster in
# project $PROJECT_ID, start knative in it, run the tests and delete the
# cluster.

source $(dirname $0)/../e2e-common.sh

# Skip installing istio as an add-on.
# Temporarily increasing the cluster size for serving tests to rule out
# resource/eviction as causes of flakiness.
initialize --skip-istio-addon --min-nodes=10 --max-nodes=10 --perf --cluster-version=1.22 "$@"

mkdir -p "${ARTIFACTS}/mako"
echo Results downloaded to "${ARTIFACTS}/mako"

# install netstat
if (( IS_PROW )); then
      apt-get update && apt-get install -y net-tools
fi

###############################################################################################
header "Dataplane probe performance test"
kubectl delete job  dataplane-probe-deployment dataplane-probe-istio dataplane-probe-queue dataplane-probe-activator --ignore-not-found=true
kubectl delete configmap config-mako --ignore-not-found=true

kubectl create configmap config-mako --from-file=test/performance/benchmarks/dataplane-probe/dev.config

ko apply -f test/performance/benchmarks/dataplane-probe/continuous/dataplane-probe-setup.yaml
ko apply -f test/performance/benchmarks/dataplane-probe/continuous/dataplane-probe-direct.yaml

echo "waiting for test to complete"
for i in {1..600}; do
        echo -n "#"
        sleep 1
done
echo ""

PODNAME_DPD=`kubectl get pods --selector=job-name=dataplane-probe-deployment --template '{{range .items}}{{.metadata.name}}{{end}}'`
header "Results from ${PODNAME_DPD}"
# read results usage: <mako_stub_pod_name> <mako_stub_namespace> <mako_stub_port> <timeout> <retries> <retries_interval> <out_file>
test/performance/read_results.sh ${PODNAME_DPD} "default" 10001 120 100 10 "${ARTIFACTS}/mako/testrun-dataplane-probe-deployment.csv"

PODNAME_DPI=`kubectl get pods --selector=job-name=dataplane-probe-istio --template '{{range .items}}{{.metadata.name}}{{end}}'`
header "Results from ${PODNAME_DPI}"
test/performance/read_results.sh ${PODNAME_DPI} "default" 10001 120 100 10 "${ARTIFACTS}/mako/testrun-dataplane-probe-istio.csv"

PODNAME_DPQ=`kubectl get pods --selector=job-name=dataplane-probe-queue --template '{{range .items}}{{.metadata.name}}{{end}}'`
header "Results from ${PODNAME_DPQ}"
test/performance/read_results.sh ${PODNAME_DPQ} "default" 10001 120 100 10 "${ARTIFACTS}/mako/testrun-dataplane-probe-queue.csv"

PODNAME_DPA=`kubectl get pods --selector=job-name=dataplane-probe-activator --template '{{range .items}}{{.metadata.name}}{{end}}'`
header "Results from ${PODNAME_DPA}"
test/performance/read_results.sh ${PODNAME_DPA} "default" 10001 120 100 10 "${ARTIFACTS}/mako/testrun-dataplane-probe-activator.csv"

############################################################################################
# header "Deployment probe performance test"
# kubectl delete job  deployment-probe --ignore-not-found=true
# kubectl delete configmap config-mako --ignore-not-found=true

# kubectl create configmap config-mako --from-file=test/performance/benchmarks/deployment-probe/dev.config

# ko apply -f test/performance/benchmarks/deployment-probe/continuous/benchmark-direct.yaml

# echo "waiting for test to complete"
# for i in {1..600}; do
#       echo -n "#"
#       sleep 1
# done
# echo ""

# PODNAME_DP=`kubectl get pods --selector=job-name=deployment-probe --template '{{range .items}}{{.metadata.name}}{{end}}'`
# header "Results from ${PODNAME_DP}"
# read results usage: <mako_stub_pod_name> <mako_stub_namespace> <mako_stub_port> <timeout> <retries> <retries_interval> <out_file>
# test/performance/read_results.sh ${PODNAME_DP} "default" 10001 120 100 10 "${ARTIFACTS}/mako/testrun-deployment-probe.csv"

###############################################################################################
header "Load test"
kubectl delete configmap config-mako --ignore-not-found=true

kubectl create configmap config-mako --from-file=test/performance/benchmarks/load-test/dev.config

ko apply -f test/performance/benchmarks/load-test/continuous/load-test-setup.yaml

###############################################################################################
header "Load test zero "
kubectl delete job load-test-zero --ignore-not-found=true

ko apply -f test/performance/benchmarks/load-test/continuous/load-test-0-direct.yaml

echo "waiting for test to complete"
for i in {1..600}; do
        echo -n "#"
        sleep 1
done
echo ""

PODNAME_LTZ=`kubectl get pods --selector=job-name=load-test-zero --template '{{range .items}}{{.metadata.name}}{{end}}'`
header "Results from ${PODNAME_LTZ}"
# read results usage: <mako_stub_pod_name> <mako_stub_namespace> <mako_stub_port> <timeout> <retries> <retries_interval> <out_file>
test/performance/read_results.sh ${PODNAME_LTZ} "default" 10001 120 100 10 "${ARTIFACTS}/mako/testrun-load-test-zero.csv"

# clean up for the next test
kubectl delete job load-test-zero --ignore-not-found=true
kubectl delete ksvc load-test-zero  --ignore-not-found=true
echo "waiting for cleanup to complete"
for i in {1..60}; do
        echo -n "#"
        sleep 1
done
echo ""

###############################################################################################
header "Load test always"
kubectl delete job load-test-always --ignore-not-found=true

ko apply -f test/performance/benchmarks/load-test/continuous/load-test-always-direct.yaml

echo "waiting for test to complete"
for i in {1..600}; do
     echo -n "#"
     sleep 1
done
echo ""

PODNAME_LTA=`kubectl get pods --selector=job-name=load-test-always --template '{{range .items}}{{.metadata.name}}{{end}}'`
header "Results from ${PODNAME_LTA}"
# read results usage: <mako_stub_pod_name> <mako_stub_namespace> <mako_stub_port> <timeout> <retries> <retries_interval> <out_file>
test/performance/read_results.sh ${PODNAME_LTA} "default" 10001 120 100 10 "${ARTIFACTS}/mako/testrun-load-test-always.csv"

# clean up for the next test
kubectl delete job load-test-always --ignore-not-found=true
kubectl delete ksvc load-test-always --ignore-not-found=true
echo "waiting for cleanup to complete"
for i in {1..60}; do
      echo -n "#"
      sleep 1
done
echo ""

#############################################################################################
header "Load test 200"
kubectl delete job load-test-200 --ignore-not-found=true

ko apply -f test/performance/benchmarks/load-test/continuous/load-test-200-direct.yaml

echo "waiting for test to complete"
for i in {1..600}; do
      echo -n "#"
      sleep 1
done
echo ""

PODNAME_LT2=`kubectl get pods --selector=job-name=load-test-200 --template '{{range .items}}{{.metadata.name}}{{end}}'`
header "Results from ${PODNAME_LT2}"
# read results usage: <mako_stub_pod_name> <mako_stub_namespace> <mako_stub_port> <timeout> <retries> <retries_interval> <out_file>
test/performance/read_results.sh ${PODNAME_LT2} "default" 10001 120 100 10 "${ARTIFACTS}/mako/load-test-200.csv"

# clean up for the next test
kubectl delete job load-test-200 --ignore-not-found=true
kubectl delete ksvc load-test-200  --ignore-not-found=true
echo "waiting for cleanup to complete"
for i in {1..60}; do
      echo -n "#"
      sleep 1
done
echo ""

###############################################################################################
#header "Rollout probe performance test"
#kubectl delete configmap config-mako --ignore-not-found=true
#
#kubectl create configmap config-mako --from-file=test/performance/benchmarks/rollout-probe/dev.config
#
#ko apply -f test/performance/benchmarks/rollout-probe/continuous/rollout-probe-setup.yaml
#
################################################################################################
#header "Rollout probe performance test with activator"
#kubectl delete job rollout-probe-activator-with-cc --ignore-not-found=true
#
#ko apply -f test/performance/benchmarks/rollout-probe/continuous/rollout-probe-activator-direct.yaml
#
#echo "waiting for test to complete"
#for i in {1..600}; do
#      echo -n "#"
#      sleep 1
#done
# echo ""
#
#PODNAME_RPACC=`kubectl get pods --selector=job-name=rollout-probe-activator-with-cc --template '{{range .items}}{{.metadata.name}}{{end}}'`
#header "Results from ${PODNAME_RPACC}"
#test/performance/read_results.sh ${PODNAME_RPACC} "default" 10001 120 100 10 "${ARTIFACTS}/mako/testrun-rollout-probe-activator-with-cc.csv"
# #clean up for the next test
#kubectl delete job rollout-probe-activator-with-cc --ignore-not-found=true
#kubectl delete ksvc activator-with-cc --ignore-not-found=true
#echo "waiting for cleanup to complete"
#for i in {1..60}; do
#      echo -n "#"
#      sleep 1
#done
# echo ""
#
###############################################################################################
#header "Rollout probe performance test with activator lin"
#kubectl delete job rollout-probe-activator-with-cc-lin --ignore-not-found=true
#
#ko apply -f test/performance/benchmarks/rollout-probe/continuous/rollout-probe-activator-lin-direct.yaml
#
#echo "waiting for test to complete"
#for i in {1..600}; do
#        echo -n "#"
#        sleep 1
#done
# echo ""
#
#PODNAME_RPALIN=`kubectl get pods --selector=job-name=rollout-probe-activator-with-cc-lin --template '{{range .items}}{{.metadata.name}}{{end}}'`
#header "Results from ${PODNAME_RPALIN}"
# read results usage: <mako_stub_pod_name> <mako_stub_namespace> <mako_stub_port> <timeout> <retries> <retries_interval> <out_file>
#test/performance/read_results.sh ${PODNAME_RPALIN} "default" 10001 120 100 10 "${ARTIFACTS}/mako/testrun-rollout-probe-activator-with-cc-lin.csv"
#
## clean up for the next test
#kubectl delete job rollout-probe-activator-with-cc-lin --ignore-not-found=true
#kubectl delete ksvc activator-with-cc-lin --ignore-not-found=true
#echo "waiting for cleanup to complete"
#for i in {1..60}; do
#        echo -n "#"
#        sleep 1
#done
# echo ""
#
################################################################################################
#header "Rollout probe performance test with queue"
#kubectl delete job rollout-probe-queue-with-cc --ignore-not-found=true
#
#ko apply -f test/performance/benchmarks/rollout-probe/continuous/rollout-probe-queue-direct.yaml
#
#echo "waiting for test to complete"
#for i in {1..600}; do
#        echo -n "#"
#        sleep 1
#done
# echo ""
#
#PODNAME_RPQ=`kubectl get pods --selector=job-name=rollout-probe-queue-with-cc --template '{{range .items}}{{.metadata.name}}{{end}}'`
#header "Results from ${PODNAME_RPQ}"
# read results usage: <mako_stub_pod_name> <mako_stub_namespace> <mako_stub_port> <timeout> <retries> <retries_interval> <out_file>
#test/performance/read_results.sh ${PODNAME_RPQ} "default" 10001 120 100 10 "${ARTIFACTS}/mako/testrun-rollout-probe-queue-with-cc.csv"
#
## clean up for the next test
#kubectl delete job rollout-probe-queue-with-cc --ignore-not-found=true
#kubectl delete ksvc queue-proxy-with-cc --ignore-not-found=true
#echo "waiting for cleanup to complete"
#for i in {1..60}; do
#        echo -n "#"
#        sleep 1
#done
#echo ""

###############################################################################################
header "Scale from Zero performance test"
kubectl delete job scale-from-zero-1 scale-from-zero-5 scale-from-zero-25 --ignore-not-found=true
kubectl delete configmap config-mako --ignore-not-found=true

kubectl create configmap config-mako --from-file=test/performance/benchmarks/scale-from-zero/dev.config

ko apply -f test/performance/benchmarks/scale-from-zero/continuous/scale-from-zero-direct.yaml

echo "waiting for test to complete"
for i in {1..120}; do
      echo -n "#"
      sleep 1
done
echo ""

PODNAME1=`kubectl get pods --selector=job-name=scale-from-zero-1 --template '{{range .items}}{{.metadata.name}}{{end}}'`
header "Results from ${PODNAME1}"
# read results usage: <mako_stub_pod_name> <mako_stub_namespace> <mako_stub_port> <timeout> <retries> <retries_interval> <out_file>
test/performance/read_results.sh ${PODNAME1} "default" 10001 120 100 10 "${ARTIFACTS}/mako/testrun-scale-from-zero-1.csv"

PODNAME5=`kubectl get pods --selector=job-name=scale-from-zero-5 --template '{{range .items}}{{.metadata.name}}{{end}}'`
header "Results from ${PODNAME5}"
test/performance/read_results.sh ${PODNAME5} "default" 10001 120 100 10 "${ARTIFACTS}/mako/testrun-scale-from-zero-5.csv"

PODNAME25=`kubectl get pods --selector=job-name=scale-from-zero-25 --template '{{range .items}}{{.metadata.name}}{{end}}'`
header "Results from ${PODNAME25}"
test/performance/read_results.sh ${PODNAME25} "default" 10001 120 100 10 "${ARTIFACTS}/mako/testrun-scale-from-zero-25.csv"

success
