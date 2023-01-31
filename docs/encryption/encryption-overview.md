# Knative Serving Encryption
There are two layers where Knative Serving can provide encryption
* HTTPS on the ingress layer to the cluster
* HTTPS on the cluster internal components

## Visualization
![Visualization of Knative encryption](./encryption-overview.drawio.svg)

## HTTPS on the ingress layer
On this layer Knative Serving provides two modes:
* Provide certificates manually, refer to the [existing docs](https://knative.dev/docs/serving/using-a-tls-cert/).
* Provide certificates automatically using `cert-manager`, refer to the [existing docs](https://knative.dev/docs/serving/using-auto-tls/).


## HTTPS on the cluster internal components
**Warning: Alpha feature**

This is currently `work-in-progress` and tracked in https://github.com/knative/serving/issues/11906. You can experiment with this feature using:
* an ingress layer that already supports the feature (e.g. Kourier or Contour)
* Set `internal-encryption: "true"` in the `config-network` configmap

