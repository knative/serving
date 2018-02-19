# Prime Calculator Demo

Prime Calculator demo walks through deployment of a 'dockerized' application to the Elafros service. In this demo we will use a sample `golang` REST application that takes a number as an input and calculates the highest prime number that lower than that number. This application is particularly helpful in demonstrating Elafros app scaling as it provides an easy way of increasing the individual request execution time by passing higher max number argument into prime calculator (see [Demo](##Demo))

> In this demo we will assume access to existing Elafros service. If not, consult [README.md](https://github.com/google/elafros/blob/master/README.md) on how to deploy one.

## Application Code

In this demo we are going to use a app called [rester-tester](https://github.com/mchmarny/rester-tester). It's important to point out that this application doesn't use any 'special' Elafros components nor does it have any Elafros SDK dependencies. You can the code of this application in [this GitHub repo](https://github.com/mchmarny/rester-tester)

## Deployment

You can now deploy the `rester-tester` app to the Elafros service from it's current GitHub repo using `kubectl`. From the `prime-calculator` directory execute: 

```
kubectl apply -f manifest.yaml
```

## Setup

To confirm that the app deployed, you can check for the Elafros service using `kubectl`. First, is there an ingress service:

```
kubectl get ing
```

Sometimes the newly deployed app may take few seconds to initialize. You can check its status like this

```
kubectl -n default get pods
```

The Elafros ingress service will automatically be assigned an IP so let's capture that IP so we can use it in subsequent `curl` commands

```shell
export SERVICE_IP=`kubectl get ing prime-ela-ingress \
  -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`
```

If your cluster is running outside a cloud provider (for example on Minikube),
your ingress will never get an address. In that case, use the istio `hostIP` and `nodePort` as the service IP:

```shell
export SERVICE_IP=$(kubectl get po -l istio=ingress -n istio-system -o 'jsonpath={.items[0].status.hostIP}'):$(kubectl get svc istio-ingress -n istio-system -o 'jsonpath={.spec.ports[?(@.port==80)].nodePort}')
```

## Demo

In this demo we are going to execute a basic max prime number calculator

### Request

> To make the JSON service responses more readable consider installing [jq](https://stedolan.github.io/jq/), makes JSON pretty

To get max prime using defaults

```
curl http://localhost:8080/prime
```

To pass max number as argument to prime calculator 

> Note, the higher the number the longer that request will run. Eventually, there will be timeout :( 

```
curl http://localhost:8080/prime/50000000
```

##### Response

```
{
     "prime": {
          "max": 50000000,
          "host_name": "056504ae4039",
          "val": 49999991
     }
}
```
