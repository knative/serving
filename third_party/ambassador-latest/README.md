### Ambassador

`ambassador-rbac.yaml` and `ambassador-service.yaml` are required to install
Ambassador. These need to be updated every time Ambassador's version is bumped.
Detailed instructions on installing Ambassador are available
[here](https://www.getambassador.io/user-guide/getting-started/).

These files are hosted at:

- https://www.getambassador.io/yaml/ambassador/ambassador-crds.yaml
- https://www.getambassador.io/yaml/ambassador/ambassador-rbac.yaml

After updating the files, make sure any other modifications required to run
Knative with Ambassador are made as well. For instance:

- Set the environment variable `AMBASSADOR_KNATIVE_SUPPORT` to `true` in the
  deployment `ambassador`.
- Update the namespace in the ClusterRoleBinding `ambassador` to `ambassador`.
  It should look like:

```yaml
apiVersion: rbac.authorization.k8s.io/v1beta1
kind: ClusterRoleBinding
metadata:
  name: ambassador
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: ambassador
subjects:
  - kind: ServiceAccount
    name: ambassador
    namespace: ambassador
```

- Ambassador has a backoff when it receives too many Envoy configuration changes
  at the same time and it's using a lot of its available memory. Update the
  resource requests and limits for the deployment to something larger for our
  end-to-end tests:

```yaml
# ...
resources:
  limits:
    cpu: 2
    memory: 1Gi
  requests:
    cpu: 500m
    memory: 1Gi
```

- (Temporarily) set the environment variable `AMBASSADOR_FAST_RECONFIGURE` to
  `true` in the deployment.
- Set the environment variable `AMBASSADOR_DRAIN_TIME` to a smaller value, like
  15. See
  [the Ambassador documentation](https://www.getambassador.io/docs/edge-stack/latest/topics/running/scaling/)
  for performance recommendations.

Make sure the modifications are done as per the
[installation documentation](https://knative.dev/docs/install/).
