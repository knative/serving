# gRPC Ping

A simple gRPC server written in Go that you can use for testing.

## Prerequisites

1. [Install Elafros](https://github.com/elafros/install/blob/master/README.md)
1. Install [docker](https://www.docker.com/)

## Build and run the gRPC server

Build and run the gRPC server. This command will build the server and use `kubectl` to apply the configuration.

```
REPO="gcr.io/<your-project-here>"

# Build and publish the container, run from the root directory.
docker build \
  --build-arg SAMPLE=grpc-ping \
  --tag "${REPO}/sample/grpc-ping" \
  --file=sample/Dockerfile.golang .
docker push "${REPO}/sample/grpc-ping"

# Replace the image reference with our published image.
perl -pi -e "s@github.com/elafros/elafros/sample/grpc-ping@${REPO}/sample/grpc-ping@g" sample/grpc-ping/*.yaml

# Deploy the Elafros sample
kubectl apply -f sample/grpc-ping/sample.yaml

```

## Use the client to stream messages to the gRPC server

1. Fetch the created ingress hostname and IP.

```
# Put the Ingress Host name into an environment variable.
export SERVICE_HOST=`kubectl get route grpc-ping -o jsonpath="{.status.domain}"`

# Put the Ingress IP into an environment variable.
export SERVICE_IP=`kubectl get ingress grpc-ping-ela-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`
```

1. Use the client to send message streams to the gRPC server

```
go run client/client.go -server_addr="$SERVICE_IP:80" -server_host_override="$SERVICE_HOST"
```
