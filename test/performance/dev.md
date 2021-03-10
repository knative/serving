# How to start with Mako

This document describes how to start running writing benchmarks with mako on
GKE.

## Preconditions

- Assume cluster exists with istio lean and serving installed.
- ko is installed
- gcloud is installed

## Steps

Take `dataplane-probe` benchmark for example:

1. Edit
   [mako config](https://github.com/knative/serving/blob/main/test/performance/config/config-mako.yaml)
   to attach your desired tags, see the `_example` stanza for how. Then apply
   it:

   ```shell
   kubectl apply -f test/performance/config/config-mako.yaml
   ```

1. Create an IAM service account:

   ```shell
   gcloud iam service-accounts create mako-upload
   ```

1. Add the IAM service account
   [here](https://github.com/knative/serving/blob/d73bb8378cab8bb0c1825aa9802bea9ea2e6cb26/test/performance/benchmarks/dataplane-probe/continuous/dev.config#L20)
   (A current owner must apply this before things will work and the SA must be
   allowed) then run:

   ```shell
   mako update_benchmark test/performance/benchmarks/dataplane-probe/dev.config
   ```

1. Create a JSON key for it.

   ```shell
   gcloud iam service-accounts keys create robot.json \
     --iam-account=mako-upload@${PROJECT_ID}.iam.gserviceaccount.com
   ```

1. Create a secret with it:

   ```shell
   kubectl create secret generic mako-secrets --from-file=./robot.json
   ```

1. Patch Istio:

   ```shell
   # This command will fail if no HPA for istio-ingressgateway exists.
   # If that is the case, the error can be ignored.
   kubectl patch hpa -n istio-system istio-ingressgateway \
     --patch '{"spec": {"minReplicas": 10, "maxReplicas": 10}}'

   kubectl patch deploy -n istio-system istio-ingressgateway \
     --patch '{"spec": {"replicas": 10}}'

   # This command will fail if no HPA for cluster-local-gateway exists.
   # If that is the case, the error can be ignored.
   kubectl patch hpa -n istio-system cluster-local-gateway \
     --patch '{"spec": {"minReplicas": 10, "maxReplicas": 10}}'

   kubectl patch deploy -n istio-system cluster-local-gateway \
     --patch '{"spec": {"replicas": 10}}'
   ```

1. Patch Knative:

   ```shell
   kubectl patch hpa -n knative-serving activator --patch '{"spec": {"minReplicas": 10}}'
   ```

1. Apply `setup` for benchmark if there's any. Take `dataplane-probe` benchmark
   for example:

   ```shell
   ko apply -f test/performance/benchmarks/dataplane-probe/continuous/dataplane-probe-setup.yaml
   ```

1. Wait for above to stabilize

1. Apply the benchmark cron. Take `dataplane-probe` benchmark for example:

   ```gcloud
   ko apply -f test/performance/benchmarks/dataplane-probe/continuous/dataplane-probe.yaml
   ```
