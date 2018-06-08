# Autoscale Sample

A demonstration of the autoscaling capabilities of an Knative Serving Revision.

## Prerequisites

1. [Install Knative Serving](https://github.com/knative/install/blob/master/README.md)
1. Install [docker](https://www.docker.com/)

## Setup

Build the autoscale app container and publish it to your registry of choice:

```shell
REPO="gcr.io/<your-project-here>"

# Build and publish the container, run from the root directory.
docker build \
  --build-arg SAMPLE=autoscale \
  --tag "${REPO}/sample/autoscale" \
  --file=sample/Dockerfile.golang .
docker push "${REPO}/sample/autoscale"

# Replace the image reference with our published image.
perl -pi -e "s@github.com/knative/serving/sample/autoscale@${REPO}/sample/autoscale@g" sample/autoscale/sample.yaml

# Deploy the Knative Serving sample
kubectl apply -f sample/autoscale/sample.yaml

```

Export your Ingress IP as SERVICE_IP.

```shell
# Put the Ingress Host name into an environment variable.
export SERVICE_HOST=`kubectl get route autoscale-route -o jsonpath="{.status.domain}"`

# Put the Ingress IP into an environment variable.
export SERVICE_IP=`kubectl get ingress autoscale-route-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`
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

Watch the Knative Serving deployment pod count increase.

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
kubectl delete -f sample/autoscale/sample.yaml
```
