# gRPC Ping

A simple gRPC server written in Go that you can use for testing.

This sample is dependent on [this issue](https://github.com/knative/serving/issues/1047) to be complete.

## Prerequisites

1. [Install Knative](https://github.com/knative/install/blob/master/README.md)
1. Install [docker](https://www.docker.com/)

## Build and run the gRPC server

Build and run the gRPC server. This command will build the server and use `kubectl` to apply the configuration.

```
REPO="gcr.io/<your-project-here>"

# Build and publish the container, run from the root directory.
docker build \
  --build-arg SAMPLE=grpc-ping \
  --build-arg BUILDTAG=grpcping \
  --tag "${REPO}/sample/grpc-ping" \
  --file=sample/grpc-ping/Dockerfile .
docker push "${REPO}/sample/grpc-ping"

# Replace the image reference with our published image.
perl -pi -e "s@github.com/knative/serving/sample/grpc-ping@${REPO}/sample/grpc-ping@g" sample/grpc-ping/*.yaml

# Deploy the Knative sample
kubectl apply -f sample/grpc-ping/sample.yaml

```

## Use the client to stream messages to the gRPC server

1. Fetch the created ingress hostname and IP.

```
# Put the Host name into an environment variable.
export SERVICE_HOST=`kubectl get route grpc-ping -o jsonpath="{.status.domain}"`

# Put the ingress IP into an environment variable.
export SERVICE_IP=`kubectl get svc knative-ingressgateway -n istio-system -o jsonpath="{.status.loadBalancer.ingress[*].ip}"`
```

1. Use the client to send message streams to the gRPC server

```
go run -tags=grpcping sample/grpc-ping/client/client.go -server_addr="$SERVICE_IP:80" -server_host_override="$SERVICE_HOST"
```
