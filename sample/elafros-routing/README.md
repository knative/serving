# Routing across Elafros Services

An example that shows how to configure Ingress and RouteRule to direct traffic based on URI. You can set up other routing rules (e.g. routing based on header, etc.) based on this example.

In this sample, we set up two web servers: "search" server and "login" server, which simplely read
in an env variable 'TARGET' and prints "Search service TARGET env: ${TARGET}" and "Login service TARGET env: ${TARGET}" respectively.

Then we set up an Ingress with placeholder service and a RouteRule which direct traffic to these two servers based on URI.

## Prerequisites

1. [Install Elafros](https://github.com/elafros/install/blob/master/README.md)
1. Install [docker](https://www.docker.com/)

## Setup

Build the app container and publish it to your registry of choice:

```shell
REPO="gcr.io/<your-project-here>"

# Build and publish the container, run from the root directory.
docker build -t "${REPO}/sample/elafros-routing/search" \
--file=sample/elafros-routing/Dockerfile \
--build-arg INPUT_SERVICE=search_service --no-cache .

docker push "${REPO}/sample/elafros-routing/search"

docker build -t "${REPO}/sample/elafros-routing/login" \
--file=sample/elafros-routing/Dockerfile \
--build-arg INPUT_SERVICE=login_service --no-cache .

docker push "${REPO}/sample/elafros-routing/login"

# Replace the image reference with our published image.
perl -pi -e "s@github.com/elafros/elafros/sample/elafros-routing@${REPO}/sample/elafros-routing@g" sample/elafros-routing/*.yaml

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
NAME                                 HOSTS                                                                                         ADDRESS        PORTS
login-service-route-ela-ingress     login-service-route.default.demo-domain.com,*.login-service-route.default.demo-domain.com    35.229.43.224      80
search-service-route-ela-ingress    search-service-route.default.demo-domain.com,*.search-service-route.default.demo-domain.com   35.229.43.224      80
entry-ingress     entry.default.demo-domain.com                                        35.229.43.224      80
```
The login-service-route-ela-ingress and search-service-route-ela-ingress are Ingresses corresponding to "login" server and "search" server.
The "entry-ingress" is the Ingress entry that can route traffic to "login" and "search" servers based on URI.

You can directly access "search" server by running
```shell
curl http://35.229.43.224 --header "Host:search-service-route.default.demo-domain.com"
```
You should see
```
Search service TARGET env: search-target!
```
Similarly, you can also directly access "login" server.

You can also send request to "search" server and "login" server through "entry-ingress" by setting different URI.
```shell
# send request to search server
curl http://35.229.43.224/search --header "Host:entry.default.demo-domain.com"

# send request to login server
curl http://35.229.43.224/login --header "Host:entry.default.demo-domain.com"
```
You should get the same results as you directly access the servers.


## How It Works
4 components are defined in order to implement the routing.
1. Service "entry-service": a placeholder service needed for setting up Ingress and RouteRule
2. Ingress "entry-ingress": a new Ingress entry for routing traffic.
3. RouteRule "entry-route-search": a RouteRule that checks if request has URI "/search", and forwards the request to "search" server.
4. RouteRule "entry-route-login": a RouteRule that checks if request has URI "/login", and forwards the request to "login" server.

When an external request reaches "entry-ingress" Ingress, the Ingress proxy will check if it has "/search" or "/login" URI. If it has, then the host of request will be rewritten into the host of "search" server or "login" server correspondingly, which actually resets the final destination of the request. The host rewriting is defined in RouteRule ""entry-route-search" and "entry-route-login".
The request with updated host will be forwarded to Ingress again. The Ingress proxy checks the updated host, and forwards it to "search" or "login" server according to its host setting.

## Cleaning up

To clean up the sample resources:

```shell
kubectl delete -f sample/elafros-routing/sample.yaml

kubectl delete -f sample/elafros-routing/routing.yaml
```
