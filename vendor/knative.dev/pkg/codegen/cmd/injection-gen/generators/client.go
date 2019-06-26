/*
Copyright 2019 The Kubernetes Authors.

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

// clientGenerator produces a file of listers for a given GroupVersion and
// type.
type clientGenerator struct {
	generator.DefaultGen
	outputPackage    string
	imports          namer.ImportTracker
	clientSetPackage string
	filtered         bool
}

var _ generator.Generator = (*clientGenerator)(nil)

func (g *clientGenerator) Filter(c *generator.Context, t *types.Type) bool {
	// We generate a single client, so return true once.
	if !g.filtered {
		g.filtered = true
		return true
	}
	return false
}

func (g *clientGenerator) Namers(c *generator.Context) namer.NameSystems {
	return namer.NameSystems{
		"raw": namer.NewRawNamer(g.outputPackage, g.imports),
	}
}

func (g *clientGenerator) Imports(c *generator.Context) (imports []string) {
	imports = append(imports, g.imports.ImportLines()...)
	return
}

func (g *clientGenerator) GenerateType(c *generator.Context, t *types.Type, w io.Writer) error {
	sw := generator.NewSnippetWriter(w, c, "{{", "}}")

	klog.V(5).Infof("processing type %v", t)

	m := map[string]interface{}{
		"clientSetNewForConfigOrDie": c.Universe.Function(types.Name{Package: g.clientSetPackage, Name: "NewForConfigOrDie"}),
		"clientSetInterface":         c.Universe.Type(types.Name{Package: g.clientSetPackage, Name: "Interface"}),
		"injectionRegisterClient":    c.Universe.Function(types.Name{Package: "knative.dev/pkg/injection", Name: "Default.RegisterClient"}),
		"restConfig":                 c.Universe.Type(types.Name{Package: "k8s.io/client-go/rest", Name: "Config"}),
		"loggingFromContext": c.Universe.Function(types.Name{
			Package: "knative.dev/pkg/logging",
			Name:    "FromContext",
		}),
	}

	sw.Do(injectionClient, m)

	return sw.Error()
}

var injectionClient = `
func init() {
	{{.injectionRegisterClient|raw}}(withClient)
}

// Key is used as the key for associating information with a context.Context.
type Key struct{}

func withClient(ctx context.Context, cfg *{{.restConfig|raw}}) context.Context {
	return context.WithValue(ctx, Key{}, {{.clientSetNewForConfigOrDie|raw}}(cfg))
}

// Get extracts the {{.clientSetInterface|raw}} client from the context.
func Get(ctx context.Context) {{.clientSetInterface|raw}} {
	untyped := ctx.Value(Key{})
	if untyped == nil {
		{{.loggingFromContext|raw}}(ctx).Fatalf(
			"Unable to fetch %T from context.", ({{.clientSetInterface|raw}})(nil))
	}
	return untyped.({{.clientSetInterface|raw}})
}
`
