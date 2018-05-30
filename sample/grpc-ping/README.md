# gRPC Ping

A simple gRPC server written in Go that you can use for testing.

## Prerequisites

1. [Install Elafros](https://github.com/elafros/install/blob/master/README.md)
1. Install [docker](https://www.docker.com/)
1. Install [ko](https://github.com/google/go-containerregistry/tree/master/cmd/ko)

## Build and run the gRPC server

Build and run the gRPC server. This command will build the server and use `kubectl` to apply the configuration.

```
ko apply -f configuration.yml
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
