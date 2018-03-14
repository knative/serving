# Autoscale Sample

A demonstration of the autoscaling capabilities of an Elafros Revision.

## Prerequisites

1. [Setup your development environment](../../DEVELOPMENT.md#getting-started)
2. [Start Elafros](../../README.md#start-elafros)

## Setup

Deploy the autoscale app to Elafros from the root directory.

```shell
bazel run sample/autoscale:everything.create
```

Export your Ingress IP as SERVICE_IP.

```shell
export SERVICE_IP=`kubectl get ingress autoscale-route-ela-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`
```

Request the largest prime less than 40,000,000 from the autoscale app.  Note that it consumes about 1 cpu/sec.

```shell
time curl --header 'Host:autoscale.myhost.net' http://${SERVICE_IP?}/primes/40000000
```

## Running

Ramp up a bunch of traffic on the autoscale app (about 300 QPS).

```shell
for i in `seq 10 10 1000`; do
  kubectl run wrk-$i \
    --image josephburnett/wrk2:latest \
    --restart Never \
    --image-pull-policy=Always \
    -l "app=wrk" -n wrk \
    -- \
    -c10 -t10 -d10m -R1000 -s /wrk2/scripts/points.lua \
    -H 'Host: autoscale.myhost.net' \
    "http://${SERVICE_IP}/primes/40000000"
  sleep 2
done
```

Watch the Elafros deployment pod count increase.

```shell
watch kubectl get deploy
```

Look at the latency, requests/sec and success rate of each pod.

```shell
kubectl logs -n wrk -l "app=wrk" | awk ' /===DATA_POINT===/ { sum[$2] += $3; count[$2]++ } END { for (sec in sum) print sec " " sum[sec] / count[sec] }'
```

## Cleanup

```shell
kubectl delete namespace hey
bazel run sample/autoscale:everything.delete
```
