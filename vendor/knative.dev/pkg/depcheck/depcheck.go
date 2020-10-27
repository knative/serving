/*
Copyright 2020 The Knative Authors

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

// Package depcheck defines a test utility for ensuring certain packages don't
// take on heavy dependencies.
package depcheck

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

type node struct {
	importpath string
	consumers  map[string]struct{}
}

type graph map[string]node

func (g graph) contains(name string) bool {
	_, ok := g[name]
	return ok
}

// path constructs an examplary path that looks something like:
//    knative.dev/pkg/apis/duck
//    knative.dev/pkg/apis  # Also: [knative.dev/pkg/kmeta knative.dev/pkg/tracker]
//    k8s.io/api/core/v1
// See the failing example in the test file.
func (g graph) path(name string) []string {
	n := g[name]
	// Base case.
	if len(n.consumers) == 0 {
		return []string{name}
	}
	// Inductive step.
	consumers := make(sort.StringSlice, 0, len(n.consumers))
	for k := range n.consumers {
		consumers = append(consumers, k)
	}
	consumers.Sort()
	base := g.path(consumers[0])
	if len(base) > 1 { // Don't decorate the first entry, which is always an entrypoint.
		if len(consumers) > 1 {
			// Attach other consumers to the last entry in base.
			base = append(base[:len(base)-1], fmt.Sprintf("%s  # Also: %v", consumers[0], consumers[1:]))
		}
	}
	return append(base, name)
}

func buildGraph(importpaths ...string) (graph, error) {
	g := make(graph, len(importpaths))
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedDeps | packages.NeedModule,
	}, importpaths...)
	if err != nil {
		return nil, err
	}
	packages.Visit(pkgs, func(pkg *packages.Package) bool {
		g[pkg.PkgPath] = node{
			importpath: pkg.PkgPath,
			consumers:  make(map[string]struct{}),
		}
		return pkg.Module != nil
	}, func(pkg *packages.Package) {
		for _, imp := range pkg.Imports {
			if _, ok := g[imp.PkgPath]; ok {
				g[imp.PkgPath].consumers[pkg.PkgPath] = struct{}{}
			}
		}
	})
	return g, nil
}

// AssertNoDependency checks that the given import paths (the keys) do not
// depend (transitively) on certain banned imports (the values)
func AssertNoDependency(t *testing.T, banned map[string][]string) {
	t.Helper()
	for ip, banned := range banned {
		t.Run(ip, func(t *testing.T) {
			g, err := buildGraph(ip)
			if err != nil {
				t.Fatal("buildGraph(queue) =", err)
			}
			for _, dip := range banned {
				if g.contains(dip) {
					t.Errorf("%s depends on banned dependency %s\n%s", ip, dip,
						strings.Join(g.path(dip), "\n"))
				}
			}
		})
	}
}
