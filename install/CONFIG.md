# Configuring Elafros

## Serving multiple domains:

Different domain suffixes can be configured based on the route labels.  In order
to do this, update the config map named "ela-config" in the namespace
"ela-system".

In that config map, each entry maps a domain name to an equality-based label
selector.  If your route has labels that meet all requirement of the selector it
will use the corresponding domain as a suffix to its domain name.  If there are
multiple selectors matching your route labels, the one that is most specific
(has the most number of requirements) will be chosen.

For example, if your config map looks like
```
apiVersion: v1
kind: ConfigMap
metadata:
  name: ela-config
  namespace: ela-system
data:
  prod.domain.com: |
    selector:
      app: prod
  v2.staging.domain.com: |
    selector:
      app: staging
      version: v2
  # Default domain, provided without selector.
  default.domain.com: |
```

then
* when your route has label `app=prod`, then route domain will have the suffix
  `prod.domain.com`
* when your route has labels `app=staging, version=v2`, then route domain will
  have the suffix `v2.staging.domain.com`
* otherwise, it falls back to `default.domain.com`.

We require that at least one domain is provided without any selector as the
default domain suffix option.
