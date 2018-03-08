# Thumbnailer Demo

Thumbnailer demo is a walk-through example on how to deploy a 'dockerized' application to the Elafros service. In this demo we will use a sample `golang` application that takes video URL as an input and generates its thumbnail image.

> In this demo we will assume access to existing Elafros service. If not, consult [README.md](https://github.com/elafros/elafros/blob/master/README.md) on how to deploy one.

## Sample Code

In this demo we are going to use a simple `golang` REST app called [rester-tester](https://github.com/mchmarny/rester-tester). It's important to point out that this application doesn't use any 'special' Elafros components nor does it have any Elafros SDK dependencies.

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

You can now deploy the `rester-tester` app to the Elafros service using `kubectl` using the included `thumbnailer-prebuilt.yaml`.

```
# From inside this directory
kubectl apply -f thumbnailer-prebuilt.yaml
```

If you would like to publish your own copy of the container image, you can update the image reference in this file.


## Deploy (with Build)

You can also build the image as part of deployment.

First, be sure you have the `DOCKER_REPO_OVERRIDE` variable set to where you want images published (e.g. `gcr.io/foo-bar`).

Next, build a version of the yaml containing your repository name:

```shell
# From the root of this repository
$ bazel build sample/thumbnailer:thumbnailer.yaml
INFO: Analysed target //sample/thumbnailer:thumbnailer.yaml (1 packages loaded).
INFO: Found 1 target...
Target //sample/thumbnailer:thumbnailer.yaml up-to-date:
  bazel-genfiles/sample/thumbnailer/thumbnailer.yaml
INFO: Elapsed time: 0.383s, Critical Path: 0.07s
INFO: Build completed successfully, 2 total actions
```

Then you can simply issue the following `kubectl` commands:

```shell
# First, be sure you have the latest docker-build-helper template installed with:
kubectl apply -f sample/templates/docker-build-helper.yaml

# From the root of this repository
kubectl apply -f bazel-genfiles/sample/thumbnailer/thumbnailer.yaml
```

Now, if you look at the `status` of the revision, you will see that a build is in progress:

```shell
$ kubectl get revisions -o yaml
apiVersion: v1
items:
- apiVersion: elafros.dev/v1alpha1
  kind: Revision
  ...
  status:
    conditions:
    - reason: Building
      status: "False"
      type: BuildComplete
...
```

Once `BuildComplete` has a `status: "True"`, the revision will get deployed as in the Prebuilt case above.


## Demo

To confirm that the app deployed, you can check for the Elafros service using `kubectl`. First, is there an ingress service:

```
kubectl get ing
```

Sometimes the newly deployed app may take few seconds to initialize. You can check its status like this

```
kubectl -n default get pods
```

The Elafros ingress service will automatically be assigned an IP so let's capture that IP so we can use it in subsequent `curl` commands

```
export SERVICE_IP=`kubectl get ing thumb-ela-ingress \
  -o jsonpath="{.status.loadBalancer.ingress[*]['ip']}"`
```

If your cluster is running outside a cloud provider (for example on Minikube),
your ingress will never get an address. In that case, use the istio `hostIP` and `nodePort` as the service IP:

```shell
export SERVICE_IP=$(kubectl get po -l istio=ingress -n istio-system -o 'jsonpath={.items[0].status.hostIP}'):$(kubectl get svc istio-ingress -n istio-system -o 'jsonpath={.spec.ports[?(@.port==80)].nodePort}')
```

> To make the JSON service responses more readable consider installing [jq](https://stedolan.github.io/jq/), makes JSON pretty

### Ping

Let's start with a simple `ping` service

```
curl -H "Content-Type: application/json" -H "Host: thumb.googlecustomer.net" \
  http://$SERVICE_IP/ping | jq '.'
```

### Video Thumbnail

Now the video thumbnail.

```
curl -X POST -H "Content-Type: application/json" -H "Host: thumb.googlecustomer.net" \
  http://$SERVICE_IP/image -d '{"src":"https://www.youtube.com/watch?v=DjByja9ejTQ"}'  | jq '.'
```

You can then download the newly created thumbnail. Make sure to replace the image name with the one returned by the previous service

```
curl -H "Host: thumb.googlecustomer.net" \
  http://$SERVICE_IP/thumb/img_b43ffcc2-0c80-4862-8423-60ec1b4c4926.png > demo.png
```

## Final Thoughts

While we used in this demo an external application, the Elafros deployment steps would be similar for any 'dockerized' app you may already have... just copy the `thumbnailer.yaml` and change a few variables.
