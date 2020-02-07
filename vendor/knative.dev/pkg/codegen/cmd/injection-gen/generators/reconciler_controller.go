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
	outputPackage string
	imports       namer.ImportTracker
	filtered      bool

	clientPkg           string
	schemePkg           string
	informerPackagePath string
}

var _ generator.Generator = (*reconcilerControllerGenerator)(nil)

func (g *reconcilerControllerGenerator) Filter(c *generator.Context, t *types.Type) bool {
	// We generate a single client, so return true once.
	if !g.filtered {
		g.filtered = true
		return true
	}
	return false
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
		"type": t,
		"controllerImpl": c.Universe.Type(types.Name{
			Package: "knative.dev/pkg/controller",
			Name:    "Impl",
		}),
		"loggingFromContext": c.Universe.Function(types.Name{
			Package: "knative.dev/pkg/logging",
			Name:    "FromContext",
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
	}

	sw.Do(reconcilerControllerNewImpl, m)

	return sw.Error()
}

var reconcilerControllerNewImpl = `
const (
	defaultControllerAgentName = "{{.type|lowercaseSingular}}-controller"
	defaultFinalizerName       = "{{.type|lowercaseSingular}}"
)

func NewImpl(ctx context.Context, r Interface) *{{.controllerImpl|raw}} {
	logger := {{.loggingFromContext|raw}}(ctx)

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

	c := &reconcilerImpl{
		Client:  {{.clientGet|raw}}(ctx),
		Lister:  {{.type|lowercaseSingular}}Informer.Lister(),
		Recorder: recorder,
		FinalizerName: defaultFinalizerName,
		reconciler:    r,
	}
	impl := controller.NewImpl(c, logger, "{{.type|allLowercasePlural}}")

	return impl
}

func init() {
	{{.schemeAddToScheme|raw}}({{.schemeScheme|raw}})
}
`
