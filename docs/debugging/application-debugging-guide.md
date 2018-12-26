# Application Debugging Guide

You deployed your app to Knative Serving, but it isn't working as expected. Go
through this step by step guide to understand what failed.

## Check command line output

Check your deploy command output to see whether it succeeded or not. If your
deployment process was terminated, there should be an error message showing up
in the output that describes the reason why the deployment failed.

This kind of failure is most likely due to either a misconfigured manifest or
wrong command. For example, the following output says that you must configure
route traffic percent to sum to 100:

```console
Error from server (InternalError): error when applying patch:
{"metadata":{"annotations":{"kubectl.kubernetes.io/last-applied-configuration":"{\"apiVersion\":\"serving.knative.dev/v1alpha1\",\"kind\":\"Route\",\"metadata\":{\"annotations\":{},\"name\":\"route-example\",\"namespace\":\"default\"},\"spec\":{\"traffic\":[{\"configurationName\":\"configuration-example\",\"percent\":50}]}}\n"}},"spec":{"traffic":[{"configurationName":"configuration-example","percent":50}]}}
to:
&{0xc421d98240 0xc421e77490 default route-example STDIN 0xc421db0488 264682 false}
for: "STDIN": Internal error occurred: admission webhook "webhook.knative.dev" denied the request: mutation failed: The route must have traffic percent sum equal to 100.
ERROR: Non-zero return code '1' from command: Process exited with status 1
```

## Check application logs

Knative Serving provides default out-of-the-box logs for your application. After
entering `kubectl proxy`, you can go to the
[Kibana UI](http://localhost:8001/api/v1/namespaces/knative-monitoring/services/kibana-logging/proxy/app/kibana)
to search for logs. _(See [telemetry guide](../telemetry.md) for more
information on logging and monitoring features of Knative Serving.)_

### Stdout/stderr logs

To find the logs sent to `stdout/stderr` from your application in the Kibana UI:

1. Click `Discover` on the left side bar.
1. Choose `logstash-*` index pattern on the left top.
1. Input `tag: kubernetes*` in the top search bar then search.

### Request logs

To find the request logs of your application in the Kibana UI :

1. Click `Discover` on the left side bar.
1. Choose `logstash-*` index pattern on the left top.
1. Input `tag: "requestlog.logentry.istio-system"` in the top search bar then
   search.

## Check Route status

Run the following command to get the `status` of the `Route` object with which
you deployed your application:

```shell
kubectl get route <route-name> -o yaml
```

The `conditions` in `status` provide the reason if there is any failure. For
details, see Knative
[Error Conditions and Reporting](../spec/errors.md)(currently some of them are
not implemented yet).

### Check ClusterIngress/Istio routing

Run the following command to list all the cluster ingress, with their labels

```shell
kubectl get clusteringress -o=custom-columns='NAME:.metadata.name,LABELS:.metadata.labels'
NAME                   LABELS
helloworld-go-h5kd4    map[serving.knative.dev/route:helloworld-go serving.knative.dev/routeNamespace:default]
```

The labels `serving.knative.dev/route` and `serving.knative.dev/routeNamespace`
will tell exactly which Route a ClusterIngress is a child resource of. Find the
one corresponding to your Route. If a ClusterIngress does not exist, the route
controller believes that the Revisions targeted by your Route/Service isn't
ready. Please proceed to later sections to diagnose Revision readiness status.

Otherwise, run the following command to look at the ClusterIngress created for
your Route

```
kubectl get clusteringress <CLUSTERINGRESS_NAME> -o yaml
```

particularly, look at the `status:` section. If the ClusterIngress is working
correctly, we should see the condition with `type=Ready` to have `status=True`.
Otherwise, there will be error messages.

Now, if ClusterIngress shows status Ready, there must be a corresponding
VirtualService. Run the following command:

```shell
kubectl get virtualservice <CLUSTERINGRESS_NAME> -n knative-serving -o yaml
```

the network configuration in VirtualService must match that of ClusterIngress
and Route. VirtualService currently doesn't expose a Status field, so if one
exists and have matching configurations with ClusterIngress and Route, you may
want to wait a little bit for those settings to propagate.

If you are familar with Istio and `istioctl`, you may try using `istioctl` to
look deeper using Istio
[guide](https://istio.io/help/ops/traffic-management/proxy-cmd/).

### Check Ingress status

Before Knative 0.3 we use a LoadBalancer service call `knative-ingressgateway`
to handle ingress. Since Knative 0.3 we now use `istio-ingressgateway` Service.

To check the IP address of your Ingress, use

```shell
kubectl get svc -n istio-system istio-ingressgateway
```

Or replace that with `knative-ingressgateway` if you are using Knative release
older than 0.3.

If there is no external IP address, use

```shell
kubectl describe svc istio-ingressgateway -n istio-system
```

to see a reason why IP addresses weren't provisioned. Most likely it is due to a
quota issue.

## Check Revision status

If you configure your `Route` with `Configuration`, run the following command to
get the name of the `Revision` created for you deployment (look up the
configuration name in the `Route` .yaml file):

```shell
kubectl get configuration <configuration-name> -o jsonpath="{.status.latestCreatedRevisionName}"
```

If you configure your `Route` with `Revision` directly, look up the revision
name in the `Route` yaml file.

Then run

```shell
kubectl get revision <revision-name> -o yaml
```

A ready `Revision` should has the following condition in `status`:

```yaml
conditions:
  - reason: ServiceReady
    status: "True"
    type: Ready
```

If you see this condition, check the following to continue debugging:

- [Check Pod status](#check-pod-status)
- [Check application logs](#check-application-logs)
- [Check Istio routing](#check-istio-routing)

If you see other conditions, to debug further:

- Look up the meaning of the conditions in Knative
  [Error Conditions and Reporting](../spec/errors.md). Note: some of them are
  not implemented yet. An alternative is to
  [check Pod status](#check-pod-status).
- If you are using `BUILD` to deploy and the `BuildComplete` condition is not
  `True`, [check BUILD status](#check-build-status).

## Check Pod status

To get the `Pod`s for all your deployments:

```shell
kubectl get pods
```

This should list all `Pod`s with brief status. For example:

```console
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

The `conditions` in `status` provide the reason if there is any failure. To
access build logs, first execute `kubectl proxy` and then open
[Kibana UI](http://localhost:8001/api/v1/namespaces/knative-monitoring/services/kibana-logging/proxy/app/kibana).
Use any of the following filters within Kibana UI to see build logs. _(See
[telemetry guide](../telemetry.md) for more information on logging and
monitoring features of Knative Serving.)_

- All build logs: `_exists_:"kubernetes.labels.build-name"`
- Build logs for a specific build: `kubernetes.labels.build-name:"<BUILD NAME>"`
- Build logs for a specific build and step:
  `kubernetes.labels.build-name:"<BUILD NAME>" AND kubernetes.container_name:"build-step-<BUILD STEP NAME>"`
