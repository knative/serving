/*
Copyright 2020 The Knative Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package generators

import (
	"io"

	"k8s.io/gengo/generator"
	"k8s.io/gengo/namer"
	"k8s.io/gengo/types"
	"k8s.io/klog"
)

// reconcilerControllerGenerator produces a file for setting up the reconciler
// with injection.
type reconcilerControllerGenerator struct {
	generator.DefaultGen
	outputPackage  string
	imports        namer.ImportTracker
	typeToGenerate *types.Type

	groupName           string
	clientPkg           string
	schemePkg           string
	informerPackagePath string
}

var _ generator.Generator = (*reconcilerControllerGenerator)(nil)

func (g *reconcilerControllerGenerator) Filter(c *generator.Context, t *types.Type) bool {
	// Only process the type for this generator.
	return t == g.typeToGenerate
}

func (g *reconcilerControllerGenerator) Namers(c *generator.Context) namer.NameSystems {
	return namer.NameSystems{
		"raw": namer.NewRawNamer(g.outputPackage, g.imports),
	}
}

func (g *reconcilerControllerGenerator) Imports(c *generator.Context) (imports []string) {
	imports = append(imports, g.imports.ImportLines()...)
	return
}

func (g *reconcilerControllerGenerator) GenerateType(c *generator.Context, t *types.Type, w io.Writer) error {
	sw := generator.NewSnippetWriter(w, c, "{{", "}}")

	klog.V(5).Infof("processing type %v", t)

	m := map[string]interface{}{
		"type":  t,
		"group": g.groupName,
		"controllerImpl": c.Universe.Type(types.Name{
			Package: "knative.dev/pkg/controller",
			Name:    "Impl",
		}),
		"controllerReconciler": c.Universe.Type(types.Name{
			Package: "knative.dev/pkg/controller",
			Name:    "Reconciler",
		}),
		"controllerNewImpl": c.Universe.Function(types.Name{
			Package: "knative.dev/pkg/controller",
			Name:    "NewImpl",
		}),
		"loggingFromContext": c.Universe.Function(types.Name{
			Package: "knative.dev/pkg/logging",
			Name:    "FromContext",
		}),
		"ptrString": c.Universe.Function(types.Name{
			Package: "knative.dev/pkg/ptr",
			Name:    "String",
		}),
		"corev1EventSource": c.Universe.Function(types.Name{
			Package: "k8s.io/api/core/v1",
			Name:    "EventSource",
		}),
		"clientGet": c.Universe.Function(types.Name{
			Package: g.clientPkg,
			Name:    "Get",
		}),
		"informerGet": c.Universe.Function(types.Name{
			Package: g.informerPackagePath,
			Name:    "Get",
		}),
		"schemeScheme": c.Universe.Function(types.Name{
			Package: "k8s.io/client-go/kubernetes/scheme",
			Name:    "Scheme",
		}),
		"schemeAddToScheme": c.Universe.Function(types.Name{
			Package: g.schemePkg,
			Name:    "AddToScheme",
		}),
		"kubeclientGet": c.Universe.Function(types.Name{
			Package: "knative.dev/pkg/client/injection/kube/client",
			Name:    "Get",
		}),
		"typedcorev1EventSinkImpl": c.Universe.Function(types.Name{
			Package: "k8s.io/client-go/kubernetes/typed/core/v1",
			Name:    "EventSinkImpl",
		}),
		"recordNewBroadcaster": c.Universe.Function(types.Name{
			Package: "k8s.io/client-go/tools/record",
			Name:    "NewBroadcaster",
		}),
		"watchInterface": c.Universe.Type(types.Name{
			Package: "k8s.io/apimachinery/pkg/watch",
			Name:    "Interface",
		}),
		"controllerGetEventRecorder": c.Universe.Function(types.Name{
			Package: "knative.dev/pkg/controller",
			Name:    "GetEventRecorder",
		}),
		"controllerOptions": c.Universe.Type(types.Name{
			Package: "knative.dev/pkg/controller",
			Name:    "Options",
		}),
		"controllerOptionsFn": c.Universe.Type(types.Name{
			Package: "knative.dev/pkg/controller",
			Name:    "OptionsFn",
		}),
		"contextContext": c.Universe.Type(types.Name{
			Package: "context",
			Name:    "Context",
		}),
	}

	sw.Do(reconcilerControllerNewImpl, m)

	return sw.Error()
}

var reconcilerControllerNewImpl = `
const (
	defaultControllerAgentName = "{{.type|lowercaseSingular}}-controller"
	defaultFinalizerName       = "{{.type|allLowercasePlural}}.{{.group}}"
	defaultQueueName           = "{{.type|allLowercasePlural}}"
)

// NewImpl returns a {{.controllerImpl|raw}} that handles queuing and feeding work from
// the queue through an implementation of {{.controllerReconciler|raw}}, delegating to
// the provided Interface and optional Finalizer methods. OptionsFn is used to return
// {{.controllerOptions|raw}} to be used but the internal reconciler.
func NewImpl(ctx {{.contextContext|raw}}, r Interface, optionsFns ...{{.controllerOptionsFn|raw}}) *{{.controllerImpl|raw}} {
	logger := {{.loggingFromContext|raw}}(ctx)

	// Check the options function input. It should be 0 or 1.
	if len(optionsFns) > 1 {
		logger.Fatalf("up to one options function is supported, found %d", len(optionsFns))
	}

	{{.type|lowercaseSingular}}Informer := {{.informerGet|raw}}(ctx)

	recorder := {{.controllerGetEventRecorder|raw}}(ctx)
	if recorder == nil {
		// Create event broadcaster
		logger.Debug("Creating event broadcaster")
		eventBroadcaster := {{.recordNewBroadcaster|raw}}()
		watches := []{{.watchInterface|raw}}{
			eventBroadcaster.StartLogging(logger.Named("event-broadcaster").Infof),
			eventBroadcaster.StartRecordingToSink(
				&{{.typedcorev1EventSinkImpl|raw}}{Interface: {{.kubeclientGet|raw}}(ctx).CoreV1().Events("")}),
		}
		recorder = eventBroadcaster.NewRecorder({{.schemeScheme|raw}}, {{.corev1EventSource|raw}}{Component: defaultControllerAgentName})
		go func() {
			<-ctx.Done()
			for _, w := range watches {
				w.Stop()
			}
		}()
	}

	rec := &reconcilerImpl{
		Client:  {{.clientGet|raw}}(ctx),
		Lister:  {{.type|lowercaseSingular}}Informer.Lister(),
		Recorder: recorder,
		reconciler:    r,
	}
	impl := {{.controllerNewImpl|raw}}(rec, logger, defaultQueueName)

	// Pass impl to the options. Save any optional results.
	for _, fn := range optionsFns {
		opts := fn(impl)
		if opts.ConfigStore != nil {
			rec.configStore = opts.ConfigStore
		}
	}

	return impl
}

func init() {
	{{.schemeAddToScheme|raw}}({{.schemeScheme|raw}})
}
`
