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

Make sure the modifications are done as per the
[installation documentation](https://knative.dev/docs/install/).
