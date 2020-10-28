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

func (g graph) order() []string {
	order := make(sort.StringSlice, 0, len(g))
	for k := range g {
		order = append(order, k)
	}
	order.Sort()
	return order
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

// CheckNoDependency checks that the given import paths (ip) does not
// depend (transitively) on certain banned imports.
func CheckNoDependency(ip string, banned []string) error {
	g, err := buildGraph(ip)
	if err != nil {
		return fmt.Errorf("buildGraph(queue) = %v", err)
	}
	for _, dip := range banned {
		if g.contains(dip) {
			return fmt.Errorf("%s depends on banned dependency %s\n%s", ip, dip,
				strings.Join(g.path(dip), "\n"))
		}
	}
	return nil
}

// AssertNoDependency checks that the given import paths (the keys) do not
// depend (transitively) on certain banned imports (the values)
func AssertNoDependency(t *testing.T, banned map[string][]string) {
	t.Helper()
	for ip, banned := range banned {
		t.Run(ip, func(t *testing.T) {
			if err := CheckNoDependency(ip, banned); err != nil {
				t.Error("CheckNoDependency() =", err)
			}
		})
	}
}

// CheckOnlyDependencies checks that the given import path only
// depends (transitively) on certain allowed imports.
// Note: while perhaps counterintuitive we allow the value to be a superset
// of the actual imports to that folks can use a constant that holds blessed
// import paths.
func CheckOnlyDependencies(ip string, allowed map[string]struct{}) error {
	g, err := buildGraph(ip)
	if err != nil {
		return fmt.Errorf("buildGraph(queue) = %v", err)
	}
	for _, name := range g.order() {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("dependency %s of %s is not explicitly allowed\n%s", name, ip,
				strings.Join(g.path(name), "\n"))
		}
	}
	return nil
}

// AssertOnlyDependencies checks that the given import paths (the keys) only
// depend (transitively) on certain allowed imports (the values).
// Note: while perhaps counterintuitive we allow the value to be a superset
// of the actual imports to that folks can use a constant that holds blessed
// import paths.
func AssertOnlyDependencies(t *testing.T, allowed map[string][]string) {
	t.Helper()
	for ip, allow := range allowed {
		// Always include our own package in the set of allowed dependencies.
		allowed := make(map[string]struct{}, len(allow)+1)
		for _, x := range append(allow, ip) {
			allowed[x] = struct{}{}
		}
		t.Run(ip, func(t *testing.T) {
			if err := CheckOnlyDependencies(ip, allowed); err != nil {
				t.Error("CheckOnlyDependencies() =", err)
			}
		})
	}
}
