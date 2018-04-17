# Application Debugging Guide

You deployed your app to Elafros but it is not working as expected. Go through
this step by step guide to understand what failed.

## Check command line output

Check your deploy command output to see whether it succeeded or not. If your
deployment process was terminated, there should be error message showing up in
the output and describing the reason why the deployment failed.

This kind of failures is most likely due to either misconfigured manifest or wrong
command. For example, the following output says that you should configure route
traffic percent summing to 100:

```
Error from server (InternalError): error when applying patch:
{"metadata":{"annotations":{"kubectl.kubernetes.io/last-applied-configuration":"{\"apiVersion\":\"elafros.dev/v1alpha1\",\"kind\":\"Route\",\"metadata\":{\"annotations\":{},\"name\":\"route-example\",\"namespace\":\"default\"},\"spec\":{\"traffic\":[{\"configurationName\":\"configuration-example\",\"percent\":50}]}}\n"}},"spec":{"traffic":[{"configurationName":"configuration-example","percent":50}]}}
to:
&{0xc421d98240 0xc421e77490 default route-example STDIN 0xc421db0488 264682 false}
for: "STDIN": Internal error occurred: admission webhook "webhook.elafros.dev" denied the request: mutation failed: The route must have traffic percent sum equal to 100.
ERROR: Non-zero return code '1' from command: Process exited with status 1
```

## Check Route status

Run the following command to get `status` of the `Route` with which you deployed
your application:

```shell
kubectl get route <route-name> -o yaml
```

The `conditions` in `status` provide the reason if there is any failure. For
details, see Elafro
[Error Conditions and Reporting](../spec/errors.md)(currently some of them
are not implemented yet).

## Check revision status

If you configure your `Route` with `Configuration`, run the following command to
get the name of the `Revision` created for you deployment(look up the
configuration name in the `Route` yaml file):

```shell
kubectl get configuration <configuration-name> -o jsonpath="{.status.latestCreatedRevisionName}"
```

If you configure your `Route` with `Revision` directly, look up the revision
name in the `Route` yaml file.

Then run

```shell
kebuctl get revision <revision-name> -o yaml
```

A ready `Revision` should has the following condition in `status`:

```yaml
conditions:
  - reason: ServiceReady
    status: "True"
    type: Ready
```

If you see this condition, to debug further:

  1. [Check Pod status](#check-pod-status)
  1. [Check application logs](#check-application-logs)
  1. [Check Istio routing](#check-istio-routing)

If you see other conditions, to debug further:

  1. Look up the meaning of the conditions in Elafro
     [Error Conditions and Reporting](../spec/errors.md). Note: some of them
     are not implemented yet. An alternation is to
     [check Pod status](#check-pod-status).
  1. If you are using `BUILD` to deploy and the `BuidComplete` condition is not
     `True`, [check BUILD status](#check-build-status).

## Check Pod status

To get the `Pod`s for all your deployments:

```shell
kubectl get pods
```

This should list all `Pod`s with brief status. For example:

```
NAME                                                      READY     STATUS             RESTARTS   AGE
configuration-example-00001-deployment-659747ff99-9bvr4   2/2       Running            0          3h
configuration-example-00002-deployment-5f475b7849-gxcht   1/2       CrashLoopBackOff   2          36s
```

Choose one and use the following command to see detailed information for its
`status`. Some useful fields are `conditions` and `containerStatuses`:

```shell
kubectl get pod <pod-name> -o yaml

```

## Check Build status

If you are using Build to deploy, run the following command to get the Build for
your `Revision`:

```shell
kubectl get build $(kubectl get revision <revision-name> -o jsonpath="{.spec.buildName}") -o yaml
```

The `conditions` in `status` provide the reason if there is any failure. To access build logs, first execute `kubectl proxy` and then open [Kibana UI](http://localhost:8001/api/v1/namespaces/monitoring/services/kibana-logging/proxy/app/kibana). Use any of the following filters within Kibana UI to see build logs. _(See [telemetry guide](../telemetry.md) for more information on logging and monitoring features of Elafros.)_
* All build logs: `_exists_:"kubernetes.labels.build-name"`
* Build logs for a specific build: `kubernetes.labels.build-name:"<BUILD NAME>"`
* Build logs for a specific build and step: `kubernetes.labels.build-name:"<BUILD NAME>" AND kubernetes.container_name:"build-step-<BUILD STEP NAME>"`

## Check application logs
Elafros provides default out-of-box logs for your application. After executing
`kubectl proxy`, you can go to the
[Kibana UI](http://localhost:8001/api/v1/namespaces/monitoring/services/kibana-logging/proxy/app/kibana)
to search for logs. _(See [telemetry guide](../telemetry.md) for more information on logging and monitoring features of Elafros.)_

### Stdout/stderr logs

You can find the logs emitted to `stdout/stderr` from your application on
Kibana UI by following steps:

1. Click `Discover` on the left side bar.
1. Choose `logstash-*` index pattern on the left top.
1. Input `tag: kubernetes*` in the top search bar then search.

### Request logs

You can find the request logs of your application on Kibana UI by following
steps:

1. Click `Discover` on the left side bar.
1. Choose `logstash-*` index pattern on the left top.
1. Input `tag: "requestlog.logentry.istio-system"` in the top search bar then
   search.

## Check Istio routing

TBD.
