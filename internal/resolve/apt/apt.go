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

// key identifies a package across indexes.
type key struct{ name, version, arch string }

func keyOf(p *deb.Package) key {
	return key{p.Name, p.Version.String(), p.Architecture}
}

// NewIndex indexes the packages of architecture arch, such as "amd64", and of
// "all"; it leaves out the others. When several repositories have the same
// version of a package, it keeps the first, so pass the repositories' packages
// in order of preference.
func NewIndex(arch string, packages []*deb.Package) *Index {
	ix := &Index{arch: arch, packages: map[string][]*deb.Package{}, providers: map[string][]provider{}}
	seen := map[key]bool{}
	for _, p := range packages {
		k := keyOf(p)
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
	// Exclude names packages that the machines get some other way: they
	// satisfy every dependency on them, at any version, and are never added.
	// A listed package is added even when it is excluded.
	Exclude []string
}

// Exclusion is an excluded package that packages of the closure need.
type Exclusion struct {
	Name     string
	NeededBy []*deb.Package
}

// Model is the dependency model of one APT target and architecture.
type Model struct {
	index      *Index
	baseline   map[key]bool
	opts       Options
	exclude    map[string]bool
	exclusions map[string]*Exclusion
}

var _ resolve.Model[*deb.Package] = (*Model)(nil)

// NewModel returns the model of the packages in index, for machines that
// already have the baseline's packages.
func NewModel(index *Index, baseline []*deb.Package, opts Options) *Model {
	m := &Model{
		index:      index,
		baseline:   map[key]bool{},
		opts:       opts,
		exclude:    map[string]bool{},
		exclusions: map[string]*Exclusion{},
	}
	for _, p := range baseline {
		m.baseline[keyOf(p)] = true
	}
	for _, name := range opts.Exclude {
		m.exclude[name] = true
	}
	return m
}

// InBaseline reports whether the machines already have p.
func (m *Model) InBaseline(p *deb.Package) bool {
	return m.baseline[keyOf(p)]
}

// Excluded returns the excluded packages that the packages passed to Needs
// depend on, by name.
func (m *Model) Excluded() []Exclusion {
	out := make([]Exclusion, 0, len(m.exclusions))
	for _, e := range m.exclusions {
		out = append(out, *e)
	}
	slices.SortFunc(out, func(a, b Exclusion) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Needs returns the dependencies of p that the machines' apt installs:
// Pre-Depends and Depends, and Recommends when Options.Recommends is set. A
// Recommends that nothing satisfies is left out, as apt ignores it. So is a
// dependency that an excluded package satisfies.
//
// A Breaks or Conflicts on older versions of a package that has a newer one
// becomes a need for the newest version, conditioned on the older ones: if
// the machines have one of them, their apt upgrades it.
func (m *Model) Needs(p *deb.Package) ([]resolve.Need[*deb.Package], error) {
	var needs []resolve.Need[*deb.Package]
	for _, alts := range slices.Concat(p.PreDepends, p.Depends) {
		if n, ok := m.need(p, alts); ok {
			needs = append(needs, n)
		}
	}
	if m.opts.Recommends {
		for _, alts := range p.Recommends {
			n, ok := m.need(p, alts)
			if ok && slices.ContainsFunc(n.Alternatives, func(group []*deb.Package) bool { return len(group) > 0 }) {
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

// need returns the need for one dependency of p, or false when an excluded
// package satisfies it.
func (m *Model) need(p *deb.Package, alts deb.Alternatives) (resolve.Need[*deb.Package], bool) {
	n := resolve.Need[*deb.Package]{Text: alts.String(), All: m.opts.AllAlternatives}
	for _, r := range alts {
		group := m.index.candidates(r)
		names := []string{r.Name}
		for _, c := range group {
			names = append(names, c.Name)
		}
		if i := slices.IndexFunc(names, func(name string) bool { return m.exclude[name] }); i >= 0 {
			m.excluded(names[i], p)
			return n, false
		}
		n.Alternatives = append(n.Alternatives, group)
	}
	return n, true
}

// excluded records that p depends on the excluded package name.
func (m *Model) excluded(name string, p *deb.Package) {
	e := m.exclusions[name]
	if e == nil {
		e = &Exclusion{Name: name}
		m.exclusions[name] = e
	}
	if !slices.Contains(e.NeededBy, p) {
		e.NeededBy = append(e.NeededBy, p)
	}
}

// upgrade returns the need for the newest version of the package that r
// names, when r doesn't match that version but matches older ones. An
// unversioned Breaks or Conflicts can't be avoided by upgrading: apt removes
// the package instead, and nothing needs to be downloaded. Neither is an
// excluded package upgraded.
func (m *Model) upgrade(field string, r deb.Relation) (resolve.Need[*deb.Package], bool) {
	versions := m.index.packages[r.Name]
	if r.Op == deb.OpNone || m.exclude[r.Name] || !m.index.native(r.Arch) || len(versions) == 0 || r.SatisfiedBy(versions[0].Version) {
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
