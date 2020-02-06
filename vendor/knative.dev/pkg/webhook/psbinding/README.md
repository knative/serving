# "Pod Spec"-able Bindings

The `psbinding` package provides facilities to make authoring
[Bindings](https://docs.google.com/document/d/1t5WVrj2KQZ2u5s0LvIUtfHnSonBv5Vcv8Gl2k5NXrCQ/edit)
whose subjects adhere to
[`duckv1.PodSpecable`](https://github.com/knative/pkg/blob/master/apis/duck/v1/podspec_types.go#L32)
easier. The Bindings doc mentions two key elements of the controller
architecture:

1. The standard controller,
1. The mutating webhook (or "admission controller")

This package provides facilities for bootstrapping both of these elements. To
leverage the `psbinding` package, folks should adjust their Binding types to
implement `psbinding.Bindable`, which contains a variety of methods that will
look familiar to Knative controller authors with two new key methods: `Do` and
`Undo` (aka the "mutation" methods).

The mutation methods on the Binding take in
`(context.Context, *duckv1.WithPod)`, and are expected to alter the
`*duckv1.WithPod` appropriately to achieve the semantics of the Binding. So for
example, if the Binding's runtime contract is the inclusion of a new environment
variable `FOO` with some value extracted from the Binding's `spec` then in
`Do()` the `duckv1.WithPod` would be altered so that each of the `containers:`
contains:

```yaml
env:
  - name: "FOO"
    value: "<from Binding spec>"
```

... and `Undo()` would remove these variables. `Do` is invoked for active
Bindings, and `Undo` is invoked when they are being deleted, but their subjects
remain.

We will walk through a simple example Binding whose runtime contract is to mount
secrets for talking to Github under `/var/bindings/github`.
[See also](https://github.com/mattmoor/bindings#githubbinding) on which this is
based.

### `Do` and `Undo`

The `Undo` method itself is simply: remove the named secret volume and any
mounts of it:

```go
func (fb *GithubBinding) Undo(ctx context.Context, ps *duckv1.WithPod) {
	spec := ps.Spec.Template.Spec

	// Make sure the PodSpec does NOT have the github volume.
	for i, v := range spec.Volumes {
		if v.Name == github.VolumeName {
			ps.Spec.Template.Spec.Volumes = append(spec.Volumes[:i], spec.Volumes[i+1:]...)
			break
		}
	}

	// Make sure that none of the [init]containers have the github volume mount
	for i, c := range spec.InitContainers {
		for j, vm := range c.VolumeMounts {
			if vm.Name == github.VolumeName {
				spec.InitContainers[i].VolumeMounts = append(vm[:j], vm[j+1:]...)
				break
			}
		}
	}
	for i, c := range spec.Containers {
		for j, vm := range c.VolumeMounts {
			if vm.Name == github.VolumeName {
				spec.Containers[i].VolumeMounts = append(vm[:j], vm[j+1:]...)
				break
			}
		}
	}
}
```

The `Do` method is the dual of this: ensure that the volume exists, and all
containers have it mounted.

```go
func (fb *GithubBinding) Do(ctx context.Context, ps *duckv1.WithPod) {

	// First undo so that we can just unconditionally append below.
	fb.Undo(ctx, ps)

	// Make sure the PodSpec has a Volume like this:
	volume := corev1.Volume{
		Name: github.VolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: fb.Spec.Secret.Name,
			},
		},
	}
	ps.Spec.Template.Spec.Volumes = append(ps.Spec.Template.Spec.Volumes, volume)

	// Make sure that each [init]container in the PodSpec has a VolumeMount like this:
	volumeMount := corev1.VolumeMount{
		Name:      github.VolumeName,
		ReadOnly:  true,
		MountPath: github.MountPath,
	}
	spec := ps.Spec.Template.Spec
	for i := range spec.InitContainers {
		spec.InitContainers[i].VolumeMounts = append(spec.InitContainers[i].VolumeMounts, volumeMount)
	}
	for i := range spec.Containers {
		spec.Containers[i].VolumeMounts = append(spec.Containers[i].VolumeMounts, volumeMount)
	}
}
```

> Note: if additional context is needed to perform the mutation, then it may be
> attached-to / extracted-from the supplied `context.Context`.

### The standard controller

For simple Bindings (such as our `GithubBinding`), we should be able to
implement our `*controller.Impl` by directly leveraging
`*psbinding.BaseReconciler` to fully implement reconciliation.

```go
// NewController returns a new GithubBinding reconciler.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {
	logger := logging.FromContext(ctx)

	ghInformer := ghinformer.Get(ctx)
	dc := dynamicclient.Get(ctx)
	psInformerFactory := podspecable.Get(ctx)

	c := &psbinding.BaseReconciler{
		GVR: v1alpha1.SchemeGroupVersion.WithResource("githubbindings"),
		Get: func(namespace string, name string) (psbinding.Bindable, error) {
			return ghInformer.Lister().GithubBindings(namespace).Get(name)
		},
		DynamicClient: dc,
		Recorder: record.NewBroadcaster().NewRecorder(
			scheme.Scheme, corev1.EventSource{Component: controllerAgentName}),
	}
	impl := controller.NewImpl(c, logger, "GithubBindings")

	logger.Info("Setting up event handlers")

	ghInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	c.Tracker = tracker.New(impl.EnqueueKey, controller.GetTrackerLease(ctx))
	c.Factory = &duck.CachedInformerFactory{
		Delegate: &duck.EnqueueInformerFactory{
			Delegate:     psInformerFactory,
			EventHandler: controller.HandleAll(c.Tracker.OnChanged),
		},
	}

	// If our `Do` / `Undo` methods need additional context, then we can
	// setup a callback to infuse the `context.Context` here:
	//    c.WithContext = ...
	// Note that this can also set up additional informer watch events to
	// trigger reconciliation when the infused context changes.

	return impl
}
```

> Note: if customized reconciliation logic is needed (e.g. synthesizing
> additional resources), then the `psbinding.BaseReconciler` may be embedded and
> a custom `Reconcile()` defined, which can still take advantage of the shared
> `Finalizer` handling, `Status` manipulation or `Subject`-reconciliation.

### The mutating webhook

Setting up the mutating webhook is even simpler:

```go
func NewWebhook(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
		return psbinding.NewAdmissionController(ctx,
			// Name of the resource webhook.
			"githubbindings.webhook.bindings.mattmoor.dev",

			// The path on which to serve the webhook.
			"/githubbindings",

			// How to get all the Bindables for configuring the mutating webhook.
			ListAll,

			// How to setup the context prior to invoking Do/Undo.
			func(ctx context.Context, b psbinding.Bindable) (context.Context, error) {
				return ctx, nil
			},
		)
	}
}

// ListAll enumerates all of the GithubBindings as Bindables so that the webhook
// can reprogram itself as-needed.
func ListAll(ctx context.Context, handler cache.ResourceEventHandler) psbinding.ListAll {
	ghInformer := ghinformer.Get(ctx)

	// Whenever a GithubBinding changes our webhook programming might change.
	ghInformer.Informer().AddEventHandler(handler)

	return func() ([]psbinding.Bindable, error) {
		l, err := ghInformer.Lister().List(labels.Everything())
		if err != nil {
			return nil, err
		}
		bl := make([]psbinding.Bindable, 0, len(l))
		for _, elt := range l {
			bl = append(bl, elt)
		}
		return bl, nil
	}
}
```

### Putting it together

With the above defined, then in our webhook's `main.go` we invoke
`sharedmain.MainWithContext` passing the additional controller constructors:

```go
	sharedmain.MainWithContext(ctx, "webhook",
		// Our other controllers.
		// ...

		// For each binding we have our controller and binding webhook.
		githubbinding.NewController, githubbinding.NewWebhook,
	)
```
