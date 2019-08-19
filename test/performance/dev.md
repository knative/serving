# How to start with Mako

This document describes how to start running writing benchmarks with mako on
GKE.

## Preconditions

- Assume cluster exists with istio lean and serving installed.
- ko is installed
- gcloud is installed

## Steps

1. Apply
   [mako config](https://raw.githubusercontent.com/knative/serving/master/test/performance/config/config-mako.yaml)
1. Create an IAM service account:

   ```shell
   gcloud iam service-accounts create mako-upload
   ```

1. Add the IAM service account
   [here](https://github.com/knative/serving/blob/47a3a2480d58ffcc1d3fd9998849fda359ab91ff/test/performance/dataplane-probe/dev.config#L19)
   (A current owner must apply this before things will work and the SA must be
   whitelisted):

   ```shell
   mako update_benchmark test/performance/dataplane-probe/dev.config
   ```

1. Create a JSON key for it.

   ```shell
   gcloud iam service-accounts keys create robot.json  --iam-account=mako-upload@${PROJECT_ID}.iam.gserviceaccount.com
   ```

1. Create a secret with it:

   ```shell
   kubectl create secret generic service-account --from-file=./robot.json
   ```

1. Patch istio like
   [here](https://github.com/knative/serving/blob/47a3a2480d58ffcc1d3fd9998849fda359ab91ff/test/performance/tools/common.sh#L113-L116)
1. Patch knative like
   [here](https://github.com/knative/serving/blob/47a3a2480d58ffcc1d3fd9998849fda359ab91ff/test/performance/tools/common.sh#L132-L133)
1. Apply `setup` for benchmark:

   ```shell
   ko apply -f test/performance/dataplane-probe/dataplane-probe-setup.yaml
   ```

1. Wait for above to stabilize
1. Apply the benchmark cron:

   ```gcloud
   ko apply -f test/performance/dataplane-probe/dataplane-probe.yaml
   ```
