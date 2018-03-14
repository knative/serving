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
# Put the Ingress Host name into an environment variable.
export SERVICE_HOST=`kubectl get ingress autoscale-route-ela-ingress -o jsonpath="{.spec.rules[*].host}"`

# Put the Ingress IP into an environment variable.
export SERVICE_IP=`kubectl get ingress autoscale-route-ela-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`
```

Request the largest prime less than 40,000,000 from the autoscale app.  Note that it consumes about 1 cpu/sec.

```shell
time curl --header "Host:$SERVICE_HOST" http://${SERVICE_IP?}/primes/40000000
```

## Running

Ramp up a bunch of traffic on the autoscale app (about 300 QPS).

```shell
kubectl delete namespace hey --ignore-not-found && kubectl create namespace hey
for i in `seq 2 2 60`; do
  kubectl -n hey run hey-$i --image josephburnett/hey --restart Never -- \
    -n 999999 -c $i -z 2m -host $SERVICE_HOST \
    "http://${SERVICE_IP?}/primes/40000000"
  sleep 1
done
watch kubectl get pods -n hey --show-all
```

Watch the Elafros deployment pod count increase.

```shell
watch kubectl get deploy
```

Look at the latency, requests/sec and success rate of each pod.

```shell
for i in `seq 4 4 120`; do kubectl -n hey logs hey-$i ; done | less
```

## Cleanup

```shell
kubectl delete namespace hey
bazel run sample/autoscale:everything.delete
```
