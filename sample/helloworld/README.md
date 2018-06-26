# Helloworld

A simple web server written in Go that you can use for testing. It reads in an
env variable 'TARGET' and prints "Hello World: ${TARGET}!" if
TARGET is not specified, it will use "NOT SPECIFIED" as the TARGET.

## Prerequisites

1. [Install Knative Serving](https://github.com/knative/install/blob/master/README.md)
1. Install [docker](https://www.docker.com/)

## Setup

Build the app container and publish it to your registry of choice:

```shell
REPO="gcr.io/<your-project-here>"

# Build and publish the container, run from the root directory.
docker build \
  --build-arg SAMPLE=helloworld \
  --tag "${REPO}/sample/helloworld" \
  --file=sample/Dockerfile.golang .
docker push "${REPO}/sample/helloworld"

# Replace the image reference with our published image.
perl -pi -e "s@github.com/knative/serving/sample/helloworld@${REPO}/sample/helloworld@g" sample/helloworld/*.yaml

# Deploy the Knative Serving sample
kubectl apply -f sample/helloworld/sample.yaml
```

## Exploring

Once deployed, you can inspect the created resources with `kubectl` commands:

```shell
# This will show the route that we created:
kubectl get route -o yaml
```

```shell
# This will show the configuration that we created:
kubectl get configurations -o yaml
```

```shell
# This will show the Revision that was created by our configuration:
kubectl get revisions -o yaml

```

To access this service via `curl`, we first need to determine its ingress address:
```shell
watch kubectl get ingress
```

When the ingress is ready, you'll see an IP address in the ADDRESS field:

```
NAME                                 HOSTS                     ADDRESS   PORTS     AGE
route-example-ingress   demo.myhost.net             80        14s
```

Once the `ADDRESS` gets assigned to the cluster, you can run:

```shell
# Put the Ingress Host name into an environment variable.
export SERVICE_HOST=`kubectl get route route-example -o jsonpath="{.status.domain}"`

# Put the Ingress IP into an environment variable.
export SERVICE_IP=`kubectl get ingress route-example-ingress -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`
```

If your cluster is running outside a cloud provider (for example on Minikube),
your ingress will never get an address. In that case, use the istio `hostIP` and `nodePort` as the service IP:

```shell
export SERVICE_IP=$(kubectl get po -l istio=ingress -n istio-system -o 'jsonpath={.items[0].status.hostIP}'):$(kubectl get svc istio-ingress -n istio-system -o 'jsonpath={.spec.ports[?(@.port==80)].nodePort}')
```

Now curl the service IP as if DNS were properly configured:

```shell
curl --header "Host:$SERVICE_HOST" http://${SERVICE_IP}
# Hello World: shiniestnewestversion!
```

## Updating

You can update this to a new version. For example, update it with a new configuration.yaml via:
```shell
kubectl apply -f sample/helloworld/updated_configuration.yaml
```

Once deployed, traffic will shift to the new revision automatically. You can verify the new version
by checking route status:
```shell
# This will show the route that we created:
kubectl get route -o yaml
```

Or curling the service:
```shell
curl --header "Host:$SERVICE_HOST" http://${SERVICE_IP}
# Hello World: nextversion!
```

## Manual traffic splitting

You can manually split traffic to specific revisions. Get your revisions names via:
```shell
kubectl get revisions
```

```
NAME                                     AGE
p-1552447d-0690-4b15-96c9-f085e310e98d   22m
p-30e6a938-b28b-4d5e-a791-2cb5fe016d74   10m
```

Update `traffic` part in sample/helloworld/sample.yaml as:
```yaml
traffic:
  - revisionName: <YOUR_FIRST_REVISION_NAME>
    percent: 50
  - revisionName: <YOUR_SECOND_REVISION_NAME>
    percent: 50
```

Then update your change via:
```shell
kubectl apply -f sample/helloworld/sample.yaml
```

Once updated, you can verify the traffic splitting by looking at route status and/or curling
the service.

## Cleaning up

To clean up the sample service:

```shell
kubectl delete -f sample/helloworld/sample.yaml
```

## Custom Domain

Knative uses `demo-domain.com` as default domain; To use your own customized domain, there are a few steps involved.

1.Change [domain configuration](/config/config-domain.yaml) to replace `demo-domain.com` with your own domain name, for example, `foo.com`.

```
data:
  # These are example settings of domain.
  # prod-domain.com will be used for routes having app=prod.
  prod-domain.com: |
    selector:
      app: prod
  # Default value for domain, for routes that does not have app=prod labels.
  # Although it will match all routes, it is the least-specific rule so it
  # will only be used if no other domain matches.
  foo.com: |
```

2.Apply updated domain configuration.

  ```shell
  ko apply -f config/
  ```

3.[Deploy app normally](#Setup). When the ingress is ready, you'll see customized domain in HOSTS field together with assigned IP address.

```
NAME                    HOSTS                                                                       ADDRESS        PORTS     AGE
route-example-ingress   route-example.default.foo.com,*.route-example.default.foo.com   35.237.28.44   80        2m
```

4.Update DNS to point HOSTS to IP address.

    1. If the domain is not registered with Google, you need to [update the NS records for your domain with your registrar](https://cloud.google.com/dns/update-name-servers).
    1. Create A record via [GCP DNS](https://pantheon.corp.google.com/net-services/dns) to map `route-example.default.foo.com` to `35.237.28.44`; Step by step instruction is [here](https://cloud.google.com/dns/quickstart).

5.You should be able to access `http://route-example.default.foo.com` from browser. It may take a while for the above update to take effect though.
