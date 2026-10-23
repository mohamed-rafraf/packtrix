/*
Copyright 2026 The Packtrix Authors.

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

// Package apt is the dependency model of APT targets. It tells the closure
// engine which packages satisfy each dependency, following the rules of the
// apt that runs on the machines.
//
// A target has a single architecture, so its index only holds packages of
// that architecture and of "all". Multi-Arch then only shows in architecture
// qualifiers: "python3:any" is satisfied like "python3", and a qualifier that
// names another architecture, such as "libc6:i386", by nothing.
package apt

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/packtrix/packtrix/internal/deb"
	"github.com/packtrix/packtrix/internal/resolve"
)

// Index holds the packages of one target and architecture, from every
// repository of the target.
type Index struct {
	arch      string
	packages  map[string][]*deb.Package // by name, newest version first
	providers map[string][]provider     // by virtual package name
}

// provider is a package that provides a virtual package, at version when its
// Provides is versioned.
type provider struct {
	pkg     *deb.Package
	version *deb.Version
}

// NewIndex indexes the packages of architecture arch, such as "amd64", and of
// "all"; it leaves out the others. When several repositories have the same
// version of a package, it keeps the first, so pass the repositories' packages
// in order of preference.
func NewIndex(arch string, packages []*deb.Package) *Index {
	ix := &Index{arch: arch, packages: map[string][]*deb.Package{}, providers: map[string][]provider{}}
	type key struct{ name, version, arch string }
	seen := map[key]bool{}
	for _, p := range packages {
		k := key{p.Name, p.Version.String(), p.Architecture}
		if p.Architecture != arch && p.Architecture != "all" || seen[k] {
			continue
		}
		seen[k] = true
		ix.packages[p.Name] = append(ix.packages[p.Name], p)
		for _, r := range p.Provides {
			pr := provider{pkg: p}
			if r.Op == deb.OpEqual {
				pr.version = &r.Version
			}
			ix.providers[r.Name] = append(ix.providers[r.Name], pr)
		}
	}
	for _, versions := range ix.packages {
		slices.SortStableFunc(versions, func(a, b *deb.Package) int {
			return deb.Compare(b.Version, a.Version)
		})
	}
	for _, providers := range ix.providers {
		slices.SortStableFunc(providers, func(a, b provider) int {
			return cmp.Or(strings.Compare(a.pkg.Name, b.pkg.Name), deb.Compare(b.pkg.Version, a.pkg.Version))
		})
	}
	return ix
}

// Select returns the package to install for a listed package: the newest
// version that the constraint matches. A virtual package that a single
// package provides selects that package, as apt does.
func (ix *Index) Select(name string, c deb.Constraint) (*deb.Package, error) {
	versions := ix.packages[name]
	if len(versions) == 0 {
		var names []string
		for _, pr := range ix.providers[name] {
			if !slices.Contains(names, pr.pkg.Name) {
				names = append(names, pr.pkg.Name)
			}
		}
		switch len(names) {
		case 0:
			return nil, fmt.Errorf("no package %s in the target's repositories", name)
		case 1:
			return ix.Select(names[0], c)
		default:
			return nil, fmt.Errorf("%s is a virtual package; list one of the packages that provide it: %s", name, strings.Join(names, ", "))
		}
	}
	for _, p := range versions {
		if c.Matches(p.Version) {
			return p, nil
		}
	}
	available := make([]string, 0, len(versions))
	for _, p := range versions {
		available = append(available, p.Version.String())
	}
	return nil, fmt.Errorf("no version of %s matches %s; the repositories have %s", name, c, strings.Join(available, ", "))
}

// native reports whether an architecture qualifier, such as the "any" of
// "python3:any", allows packages of the index's architecture.
func (ix *Index) native(qualifier string) bool {
	return qualifier == "" || qualifier == "any" || qualifier == ix.arch
}

// candidates returns the packages that satisfy r, in order of preference: the
// real package, newest version first, then the packages that provide it.
func (ix *Index) candidates(r deb.Relation) []*deb.Package {
	if !ix.native(r.Arch) {
		return nil
	}
	var out []*deb.Package
	for _, p := range ix.packages[r.Name] {
		if r.SatisfiedBy(p.Version) {
			out = append(out, p)
		}
	}
	for _, pr := range ix.providers[r.Name] {
		// A versioned dependency is only satisfied by a versioned Provides.
		if r.Op == deb.OpNone || pr.version != nil && r.SatisfiedBy(*pr.version) {
			out = append(out, pr.pkg)
		}
	}
	return out
}

// Options are the resolve settings of a manifest that change which
// dependencies are followed.
type Options struct {
	// Recommends follows Recommends, as well as Pre-Depends and Depends.
	Recommends bool
	// AllAlternatives adds a package for every alternative of "a | b", not
	// only for the first one that something satisfies.
	AllAlternatives bool
}

// Model is the dependency model of one APT target and architecture.
type Model struct {
	index    *Index
	baseline map[*deb.Package]bool
	opts     Options
}

var _ resolve.Model[*deb.Package] = (*Model)(nil)

// NewModel returns the model of the packages in index, for machines that
// already have the baseline's packages, which come from the same index.
func NewModel(index *Index, baseline []*deb.Package, opts Options) *Model {
	m := &Model{index: index, baseline: map[*deb.Package]bool{}, opts: opts}
	for _, p := range baseline {
		m.baseline[p] = true
	}
	return m
}

// InBaseline reports whether the machines already have p.
func (m *Model) InBaseline(p *deb.Package) bool {
	return m.baseline[p]
}

// Needs returns the dependencies of p that the machines' apt installs:
// Pre-Depends and Depends, and Recommends when Options.Recommends is set. A
// Recommends that nothing satisfies is left out, as apt ignores it.
//
// A Breaks or Conflicts on older versions of a package that has a newer one
// becomes a need for the newest version, conditioned on the older ones: if
// the machines have one of them, their apt upgrades it.
func (m *Model) Needs(p *deb.Package) ([]resolve.Need[*deb.Package], error) {
	var needs []resolve.Need[*deb.Package]
	for _, alts := range slices.Concat(p.PreDepends, p.Depends) {
		needs = append(needs, m.need(alts))
	}
	if m.opts.Recommends {
		for _, alts := range p.Recommends {
			n := m.need(alts)
			if slices.ContainsFunc(n.Alternatives, func(group []*deb.Package) bool { return len(group) > 0 }) {
				needs = append(needs, n)
			}
		}
	}
	for _, field := range []struct {
		name string
		deps []deb.Alternatives
	}{{"Breaks", p.Breaks}, {"Conflicts", p.Conflicts}} {
		for _, alts := range field.deps {
			for _, r := range alts {
				if n, ok := m.upgrade(field.name, r); ok {
					needs = append(needs, n)
				}
			}
		}
	}
	return needs, nil
}

func (m *Model) need(alts deb.Alternatives) resolve.Need[*deb.Package] {
	n := resolve.Need[*deb.Package]{Text: alts.String(), All: m.opts.AllAlternatives}
	for _, r := range alts {
		n.Alternatives = append(n.Alternatives, m.index.candidates(r))
	}
	return n
}

// upgrade returns the need for the newest version of the package that r
// names, when r doesn't match that version but matches older ones. An
// unversioned Breaks or Conflicts can't be avoided by upgrading: apt removes
// the package instead, and nothing needs to be downloaded.
func (m *Model) upgrade(field string, r deb.Relation) (resolve.Need[*deb.Package], bool) {
	versions := m.index.packages[r.Name]
	if r.Op == deb.OpNone || !m.index.native(r.Arch) || len(versions) == 0 || r.SatisfiedBy(versions[0].Version) {
		return resolve.Need[*deb.Package]{}, false
	}
	var broken []*deb.Package
	for _, v := range versions[1:] {
		if r.SatisfiedBy(v.Version) {
			broken = append(broken, v)
		}
	}
	if len(broken) == 0 {
		return resolve.Need[*deb.Package]{}, false
	}
	return resolve.Need[*deb.Package]{
		Text:         field + ": " + r.String(),
		Alternatives: [][]*deb.Package{{versions[0]}},
		Condition:    broken,
	}, true
}
