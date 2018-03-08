apiVersion: elafros.dev/v1alpha1
kind: Configuration
metadata:
  name: buildpack-sample-app
  namespace: default
spec:
  build:
    source:
      git:
        url: https://github.com/cloudfoundry-samples/dotnet-core-hello-world
        branch: master
    template:
      name: buildpack
      arguments:
      - name: IMAGE
        value: &image DOCKER_REPO_OVERRIDE/buildpack-sample-app
    # - name: CACHE
    #   value: buildpack-sample-app-cache
    # volumes:
    # - name: buildpack-sample-app-cache
    #   persistentVolumeClaim:
    #     claimName: buildpack-sample-app-cache

  template:
    spec:
      serviceType: container
      containerSpec:
        image: *image
