# gitwebhook

A simple git webhook handler that demonstrates interacting with
github. 
[Modeled after GCF example](https://cloud.google.com/community/tutorials/github-auto-assign-reviewers-cloud-functions)

## Prerequisites

1. [Setup your development environment](../../DEVELOPMENT.md#getting-started)
2. [Start Elafros](../../README.md#start-elafros)
3. Decide on the DNS name that git can then call. Update sample/gitwebhook/route.yaml domainSuffix.
For example I used demostuff.aikas.org as my hostname, so my route.yaml looks like so:

```yaml
apiVersion: elafros.dev/v1alpha1
kind: Route
metadata:
  name: git-webhook
  namespace: default
spec:
  domainSuffix: demostuff.aikas.org
  traffic:
  - configuration: configuration-example
    percent: 100
```


## Running

You can deploy this to Elafros from the root directory via:
```shell
bazel run sample/gitwebhook:everything.create
```

Once deployed, you can inspect the created resources with `kubectl` commands:

```shell
# This will show the Route that we created:
kubectl get route -o yaml

# This will show the Configuration that we created:
kubectl get configurations -o yaml

# This will show the Revision that was created by our configuration:
kubectl get revisions -o yaml

```

To make this service accessible to github, we first need to determine its ingress address
(might have to wait a little while until 'ADDRESS' gets assigned):
```shell
$ watch kubectl get ingress
NAME                                 HOSTS                     ADDRESS        PORTS     AGE
git-webhook-ela-ingress              demostuff.aikas.org       35.202.30.59   80        14s
```

Once the `ADDRESS` gets assigned to the cluster, you need to assign a DNS name for that IP address.
[Using GCP DNS](https://support.google.com/domains/answer/3290350)

So, you'd need to create an A record for demostuff.aikas.org pointing to 35.202.30.59.

Then you need to go to github and [set up a webhook](https://cloud.google.com/community/tutorials/github-auto-assign-r
eviewers-cloud-functions).
For the Payload URL however, use your DNS entry you created above, so for my example it would be:
http://demostuff.aikas.org/

Create a secret that has access to the tokens. Take the Secret you used for the webhook
(secretToken) and the generated access token (accessToken) (as per the above  webhook)

```shell
echo -n "your-chosen-secret-token" > secretToken
echo -n "github-generated-access-token" > accessToken
kubectl create secret generic githubsecret --from-file=./secretToken --from-file=./accessToken
```

Then create a PR for the repo you configured the webhook for, and you'll see that the Title
will be modified with the suffix '(looks pretty legit)'

## Cleaning up

To clean up the sample service:

```shell
bazel run sample/gitwebhook:everything.delete
```
