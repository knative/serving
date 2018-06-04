# Routing across Knative Services

This example shows how to map multiple Knative services to different paths 
under a single domain name using the Istio Ingress and RouteRule concepts. 
Since Istio is a general-purpose reverse proxy, these directions can also be 
used to configure routing based on other request data such as headers, or even 
to map Knative and external resources under the same domain name.

In this sample, we set up two web services: "Search" service and "Login" 
service, which simply read in an env variable 'SERVICE_NAME' and prints 
"${SERVICE_NAME} is called". We'll then create an Ingress with host 
"example.com", and routing rules so that example.com/search maps to the Search 
service, and example.com/login maps to the Login service.

## Prerequisites

1. [Install Knative](https://github.com/knative/install/blob/master/README.md)
1. Install [docker](https://www.docker.com/)
1. Acquire a domain name. In this example, we use example.com. If you don't 
have a domain name, you can modify your hosts file (on Mac or Linux) to map 
example.com to your cluster's ingress IP.

## Setup

Build the app container and publish it to your registry of choice:

```shell
REPO="gcr.io/<your-project-here>"

# Build and publish the container, run from the root directory.
docker build \
  --build-arg SAMPLE=knative-routing \
  --tag "${REPO}/sample/knative-routing" \
  --file=sample/Dockerfile.golang .

docker push "${REPO}/sample/knative-routing"

# Replace the image reference with our published image.
perl -pi -e "s@github.com/knative/serving/sample/knative-routing@${REPO}/sample/knative-routing@g" sample/knative-routing/*.yaml

# Deploy the "Search" and "Login" services.
kubectl apply -f sample/knative-routing/sample.yaml
```

## Exploring
Once deployed, you can inspect Knative services with
```shell
kubectl get service.knative.dev
```
You should see 2 Knative services: Search and Login.
And you can inspect the corresponding Ingress with
```shell
kubectl get Ingress
```
You should see 2 Ingress objects:

```
NAME                                 HOSTS                                                                                         ADDRESS        PORTS
login-service-ela-ingress     login-service.default.demo-domain.com,*.login-service.default.demo-domain.com    35.237.65.249      80
search-service-ela-ingress    search-service.default.demo-domain.com,*.search-service.default.demo-domain.com   35.237.65.249      80
```
The login-service-ela-ingress and search-service-ela-ingress are Ingresses corresponding to "Login" service and "Search" service.

You can directly access "Search" service by running
```shell
curl http://35.237.65.249 --header "Host:search-service.default.demo-domain.com"
```
You should see
```
Search Service is called !
```
Similarly, you can also directly access "Login" service.

## Apply Custom Routing Rule
You can apply the custom routing rule defined in "routing.yaml" file with
```shell
kubectl apply -f sample/knative-routing/routing.yaml
```
The routing.yaml file will generate a new Ingress "entry-ingress" for domain 
"example.com". You can see it by running
```shell
kubectl get Ingress
```
And you should see "entry-ingress" is added into the Ingress results:
```
NAME            HOSTS         ADDRESS         PORTS     AGE
entry-ingress   example.com   35.237.65.249   80        4h
```
Now you can send request to "Search" service and "Login" service by using 
different URI.

```shell
# send request to Search service
curl http://35.237.65.249/search --header "Host:example.com"

# send request to Login service
curl http://35.237.65.249/login --header "Host:example.com"
```
You should get the same results as you directly access these services.

## Exploring Custom Routing Rule
Besides "entry-ingress" Ingress, there are another 3 objects that are 
generated: 
"entry-service" Service, "entry-route-search" RouteRule and 
"entry-route-login" RouteRule.

You can inspect the details of each objects by running:
```shell
# Check details of entry-service Service:
kubectl get Service entry-service -o yaml

# Check details of entry-route-search RouteRule
kubectl get RouteRule entry-route-search -o yaml

# Check details of entry-route-login RouteRule
kubectl get RouteRule entry-route-login -o yaml
```

## How It Works
This is the traffic flow of this sample:
![Object model](images/knative-routing-sample-flow.png)

4 components are defined in order to implement the routing.
1. Ingress "entry-ingress": a new Ingress entry for routing traffic.
2. Service "entry-service": a placeholder service needed for setting up Ingress and RouteRule
3. RouteRule "entry-route-search": a RouteRule that checks if request has URI "
/search", and forwards the request to "Search" service.
4. RouteRule "entry-route-login": a RouteRule that checks if request has URI "
/login", and forwards the request to "Login" service.

When an external request reaches "entry-ingress" Ingress, the Ingress proxy 
will check if it has "/search" or "/login" URI. If it has, then the host of 
request will be rewritten into the host of "Search" service or "Login" service 
correspondingly, which actually resets the final destination of the request. 
The host rewriting is defined in RouteRule ""entry-route-search" and "entry-route-login".
The request with updated host will be forwarded to Ingress proxy again. The 
Ingress proxy checks the updated host, and forwards it to "Search" or "Login" 
service according to its host setting.

## Cleaning up

To clean up the sample resources:

```shell
kubectl delete -f sample/knative-routing/sample.yaml

kubectl delete -f sample/knative-routing/routing.yaml
```
