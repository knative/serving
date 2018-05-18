# Routing accross Elafros Services

An example that shows how to configure Ingress and RouteRule to direct traffic based on URI. You can configure other routing rules (e.g. routing based on header, etc.) based on this example.

We set up two simple web servers: "search" server and "login" server, which simplely read
in an env variable 'TARGET' and prints "Search service TARGET env: ${TARGET}" or "Login service TARGET env: ${TARGET}" respectively.

Then we set up an Ingress with placeholder service and a RouteRule which direct traffic to these two servers based on URL.

## Prerequisites

1. [Install Elafros](https://github.com/elafros/install/blob/master/README.md)
1. Install [docker](https://www.docker.com/)

## Setup

Build the app container and publish it to your registry of choice:

```shell
REPO="gcr.io/<your-project-here>"

# Build and publish the container, run from the root directory.
docker build -t "${REPO}/sample/elafros-routing/search" --file=sample/elafros-routing/Dockerfile --build-arg INPUT_SERVICE=search_service --no-cache .

docker push "${REPO}/sample/elafros-routing/search"

docker build -t "${REPO}/sample/elafros-routing/login" --file=sample/elafros-routing/Dockerfile --build-arg INPUT_SERVICE=login_service --no-cache .

docker push "${REPO}/sample/elafros-routing/login"


# Deploy the "search" and "login" servers.
kubectl apply -f sample/elafros-routing/sample.yaml

# Deploy the routing components.
kubectl apply -f sample/elafros-routing/routing.yaml
```

## Exploring
Once deployed, you can inspect Ingress with:

```shell
kubectl get Ingress
```

You should see 3 Ingress objects:
```
NAME                                 HOSTS                                         ADDRESS      PORTS
login-service-route-ela-ingress     login-service-route.default.demo-domain.com,*.login-service-route.default.demo-domain.com    35.229.43.224  80
search-service-route-ela-ingress    search-service-route.default.demo-domain.com,*.search-service-route.default.demo-domain.com
entry-ingress     entry.default.demo-domain.com
```
The login-service-route-ela-ingress and search-service-route-ela-ingress are Ingresses corresponding to "login" server and "search" server.
The entry-ingress is the Ingress entry that can route traffic to "login" and "search" servers based on URI.

Above result should also show the IP address and exposed ports of Ingress. Or you can get them by running
```shell
kubectl get Ingress
```



## How It Works


## Cleaning up

To clean up the sample service:

```shell
kubectl delete -f sample/elafros-routing/sample.yaml

kubectl delete -f sample/elafros-routing/routing.yaml
```
