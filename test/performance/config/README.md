### Vegeta-based load generator

This directory contains a simple `vegeta`-based load generator, which can be run
with:

```shell
ko apply -f test/performance/config
```

By default, it is configured to load test the
[autoscale-go](https://github.com/knative/docs/tree/master/docs/serving/samples/autoscale-go)
sample, which must already be deployed. You can change the target by altering
the `ConfigMap` to point to a different endpoint.

### Examining output

Once the load generation pods terminate, their outputs can be examined with:

```shell
for x in $(kubectl get pods -l app=load-test -oname); do
  kubectl logs $x | python -mjson.tool
done
```

This will produce a series of pretty-printed JSON blocks like:

```json
{
  "bytes_in": {
    "mean": 38.15242083333333,
    "total": 9156581
  },
  "bytes_out": {
    "mean": 0,
    "total": 0
  },
  "duration": 240001544213,
  "earliest": "2019-06-29T22:49:57.272758595Z",
  "end": "2019-06-29T22:53:57.399043387Z",
  "errors": [
    "503 Service Unavailable",
    "502 Bad Gateway",
    "Get http://autoscale-go.default.svc.cluster.local?sleep=100: net/http: request canceled (Client.Timeout exceeded while awaiting headers)"
  ],
  "latencies": {
    "50th": 102296894,
    "95th": 29927947157,
    "99th": 30000272067,
    "max": 30186427377,
    "mean": 2483484840,
    "total": 596036361667202
  },
  "latest": "2019-06-29T22:53:57.274302808Z",
  "rate": 999.9935658205657,
  "requests": 240000,
  "status_codes": {
    "0": 12302,
    "200": 185803,
    "502": 7,
    "503": 41888
  },
  "success": 0.7741791666666666,
  "wait": 124740579
}
```
