# Knative Serving Scaling

TODO: write developer/operator facing documentation.

## Scale Bounds

There are cases when Operators need to set lower and upper bounds on the number of pods serving their apps (e.g. avoiding cold-start, control compute costs, etc).

The following annotations can be used on `configuration.revisionTemplate` or `revision` (propagated to `kpa` objects) to do exactly that:

```yaml
    # +optional
    # When not specified, the revision can scale down to 0 pods
    autoscaling.knative.dev/minScale: "2"
    # +optional
    # When not specified, there's no upper scale bound
    autoscaling.knative.dev/maxScale: "10"
```

You can also use these annotations directly on `kpa` objects.
