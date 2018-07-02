# Thumbnailer Demo

Thumbnailer demo is a walk-through example on how to deploy a 'dockerized' application to the Knative Serving service. In this demo we will use a sample `golang` application that takes video URL as an input and generates its thumbnail image.

> In this demo we will assume access to existing Knative Serving service. If not, consult [README.md](https://github.com/knative/serving/blob/master/README.md) on how to deploy one.

## Sample Code

In this demo we are going to use a simple `golang` REST app called [rester-tester](https://github.com/mchmarny/rester-tester). It's important to point out that this application doesn't use any 'special' Knative Serving components nor does it have any Knative Serving SDK dependencies.

### App code

Let's start by cloning the public `rester-tester` repository

```
git clone git@github.com:mchmarny/rester-tester.git
cd rester-tester
```

The `rester-tester` application uses [godep](https://github.com/tools/godep)` to manage its own dependencies. Go get it and restore the app dependencies

```
go get github.com/tools/godep
godep restore
```

### Test

To quickly make sure the application is ready, execute the integrated tests

```
go test ./...
```

### Run

You can now run the `rester-tester` application locally in `go` or using Docker

**Local**

> Note: to run the application locally in `go` you will need [FFmpeg](https://www.ffmpeg.org/) in your path.

```
go build
./rester-tester
```

**Docker**

When running the application locally in docker, you do not need to install `ffmpeg`, Docker will install it for you 'inside' of the Docker image

```
docker build -t rester-tester:latest .
docker run -p 8080:8080 rester-tester:latest
```

### Test

To test the thumbnailing service use `curl` to submit `src` video URL.

```
curl -X POST -H "Content-Type: application/json" http://localhost:8080/image \
     -d '{"src":"https://www.youtube.com/watch?v=DjByja9ejTQ"}'
```

## Deploy (Prebuilt)

You can now deploy the `rester-tester` app to the Knative Serving service using `kubectl` using the included `sample-prebuilt.yaml`.

```
# From inside this directory
kubectl apply -f sample-prebuilt.yaml
```

If you would like to publish your own copy of the container image, you can update the image reference in this file.


## Deploy (with Build)

You can also build the image as part of deployment. This sample uses the
[Kaniko build
template](https://github.com/knative/build-templates/blob/master/kaniko/kaniko.yaml)
in the [build-templates](https://github.com/knative/build-templates/) repo.

```shell
# Replace the token string with a suitable registry
REPO="gcr.io/<your-project-here>"
perl -pi -e "s@DOCKER_REPO_OVERRIDE@$REPO@g" sample.yaml

# Install the Kaniko build template used to build this sample (in the
# build-templates repo).
kubectl apply -f kaniko.yaml
```

Now, if you look at the `status` of the revision, you will see that a build is in progress:

```shell
$ kubectl get revisions -o yaml
apiVersion: v1
items:
- apiVersion: serving.knative.dev/v1alpha1
  kind: Revision
  ...
  status:
    conditions:
    - reason: Building
      status: "False"
      type: BuildComplete
...
```

Once `BuildComplete` has a `status: "True"`, the revision will get deployed as in the "prebuilt" case above.


## Demo

To confirm that the app deployed, you can check for the Knative Serving service using `kubectl`. First, is there an ingress service:

```
kubectl get svc knative-ingressgateway -n istio-system
NAME                     TYPE           CLUSTER-IP     EXTERNAL-IP      PORT(S)                                      AGE
knative-ingressgateway   LoadBalancer   10.23.247.74   35.203.155.229   80:32380/TCP,443:32390/TCP,32400:32400/TCP   2d
```

Sometimes the newly deployed app may take few seconds to initialize. You can check its status like this

```
kubectl -n default get pods
```

The Knative Serving ingress service will automatically be assigned an IP so let's capture that IP so we can use it in subsequent `curl` commands

```
# Put the Host name into an environment variable.
export SERVICE_HOST=`kubectl get route thumb -o jsonpath="{.status.domain}"`

# Put the ingress IP into an environment variable.
export SERVICE_IP=`kubectl get svc knative-ingressgateway -n istio-system -o jsonpath="{.status.loadBalancer.ingress[*].ip}"`
```

If your cluster is running outside a cloud provider (for example on Minikube),
your services will never get an external IP address. In that case, use the istio `hostIP` and `nodePort` as the service IP:

```shell
export SERVICE_IP=$(kubectl get po -l knative=ingressgateway -n istio-system -o 'jsonpath={.items[0].status.hostIP}'):$(kubectl get svc knative-ingressgateway -n istio-system -o 'jsonpath={.spec.ports[?(@.port==80)].nodePort}')
```

> To make the JSON service responses more readable consider installing [jq](https://stedolan.github.io/jq/), makes JSON pretty

### Ping

Let's start with a simple `ping` service

```
curl -H "Content-Type: application/json" -H "Host: $SERVICE_HOST" \
  http://$SERVICE_IP/ping | jq '.'
```

### Video Thumbnail

Now the video thumbnail.

```
curl -X POST -H "Content-Type: application/json" -H "Host: $SERVICE_HOST" \
  http://$SERVICE_IP/image -d '{"src":"https://www.youtube.com/watch?v=DjByja9ejTQ"}'  | jq '.'
```

You can then download the newly created thumbnail. Make sure to replace the image name with the one returned by the previous service

```
curl -H "Host: $SERVICE_HOST" \
  http://$SERVICE_IP/thumb/img_b43ffcc2-0c80-4862-8423-60ec1b4c4926.png > demo.png
```

## Final Thoughts

While we used in this demo an external application, the Knative Serving deployment steps would be similar for any 'dockerized' app you may already have... just copy the `thumbnailer.yaml` and change a few variables.
