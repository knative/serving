# Application Debugging step by step Guide

This document provides instructions for how to debug your applications deployed
to Elafros.

`NOTE`: It needs to be updated once relative new features are added to Elafros.

## Failures during Deployment

This kind of failures is most likely due to either misconfigured manifest or wrong
command. It terminates the deployment process and shows some error message to
you. The error message should describe the reason why this error happens. For
example, if you change the traffic `percent` in [route.yaml](../../sample/helloworld/route.yaml)
of sample helloworld app to 50 then deploy, you will get the following message:

```
Error from server (InternalError): error when applying patch:
{"metadata":{"annotations":{"kubectl.kubernetes.io/last-applied-configuration":"{\"apiVersion\":\"elafros.dev/v1alpha1\",\"kind\":\"Route\",\"metadata\":{\"annotations\":{},\"name\":\"route-example\",\"namespace\":\"default\"},\"spec\":{\"traffic\":[{\"configurationName\":\"configuration-example\",\"percent\":50}]}}\n"}},"spec":{"traffic":[{"configurationName":"configuration-example","percent":50}]}}
to:
&{0xc421d98240 0xc421e77490 default route-example STDIN 0xc421db0488 264682 false}
for: "STDIN": Internal error occurred: admission webhook "webhook.elafros.dev" denied the request: mutation failed: The route must have traffic percent sum equal to 100.
ERROR: Non-zero return code '1' from command: Process exited with status 1
```

saying that you should have route traffic percent summing to 100.

## Failures during reconciling

After your application is deployed to Elafros, you need to wait for tens of
seconds until the latest created `Revision` to become ready and get traffic
shifted to. However sometimes it fails to happen. In such situation you can use
the following methods to debug:

### Check Elafro resources

You can run the following command to see Elafro resource status:

```
kubectl describe <resource-type>/<route-name>

# example: kubectl describe route/route-example
```

Look up the meaning of the status in
[Elafro Error Conditions and Reporting](../../blob/master/docs/spec/errors.md)

### Search logs

See [Debug with logs](#debug-with-logs) section below.


## Failures during serving

If you get unexpected request results from your application, you can use the
application logs for debugging. See below.

## Debug with logs

Elafros provides default out-of-box logs for your application. You can go to the
[Kibana UI](http://localhost:8001/api/v1/namespaces/monitoring/services/kibana-logging/proxy/app/kibana)
to search for logs.

### Stdout/stderr logs

You can find the logs emitted to `stdout/stderr` from your application on
`Kibana` UI by following steps:

1. Click `Discover` on the left side bar.
1. Choose `logstash-*` index pattern on the left top.

### Request logs

You can find the request logs of your application on `Kibana` UI by following
steps:

1. Click `Discover` on the left side bar.
1. Choose `zipkin*` index pattern on the left top.

