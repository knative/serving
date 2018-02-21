# Prime Calculator Demo

Prime Calculator demo walks through deployment of a Docker image to the Elafros service. The image includes a REST app which calculates highest prime number up to the passed in maximum argument. This app is helpful in Elafros autoscaling demos as its execution time will vary based on the scale of max number argument. Higher number equals longer execution. (see [Demo](#Demo))

> In this demo we will assume access to existing Elafros service. If not, consult [README.md](https://github.com/google/elafros/blob/master/README.md) on how to deploy one.


## Deployment

From the `primer-calculator` directory deploy the Docker image to the Elafros service using `kubectl`. The app image itself resides in [Dockerhub repository](https://cloud.docker.com/app/mchmarny/repository/docker/mchmarny/primer/general).  

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

## Demo

In this demo we are going to execute a basic max prime number calculator

### Request

To get max prime using defaults

```
curl http://${SERVICE_IP}
```

To pass max number as argument to prime calculator 

```
curl http://${SERVICE_IP}/50000000
```

> Note, the higher the number the longer that request will run. Eventually, there will be timeout :( 

### Request Loop 

Now wrap this into simple loop to demo scaling 

```
while true; do sleep .1; curl http://${SERVICE_IP}/1000000; done
```

### Response


```
{
  "id":"12c98900-ecdd-4c3b-86f1-c2bb54e28569",
  "prime":{
    "max":50000000,
    "val":49999991
  },
  "host":"host0.primer.myhost.net",
  "ts":"2018-02-20 21:34:55.796323 +0000 UTC"
}
```

### Autoscaling 

> TODO: outline Cheap and Cheerful Autoscaler usage here 