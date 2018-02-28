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
time curl --header 'Host:demo.myhost.net' http://${SERVICE_IP?}/primes/40000000
```

Build the load testing container [hey](https://github.com/rakyll/hey).

```
docker build -t "${DOCKER_REPO_OVERRIDE?}/hey" sample/autoscale/hey
docker push "${DOCKER_REPO_OVERRIDE}/hey"
```

## Running

Ramp up a bunch of traffic on the autoscale app (about 300 QPS).

```shell
for i in `seq 2 2 60`; do
  kubectl -n hey run hey-$i --image "${DOCKER_REPO_OVERRIDE?}/hey" --restart Never -- \
    -n 999999 -c $i -z 2m -host 'demo.myhost.net' \
    "http://${SERVICE_IP?}/primes/40000000"
  sleep 1
done
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
kubectl delete all --all -n hey
bazel run sample/autoscale:everything.delete
```