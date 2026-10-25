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

// Package expand turns the short names of a manifest into concrete facts: for
// each target, its repositories with their URLs, suites, components and keys,
// and the packages it needs. It is the only stage that reads the catalog, so
// a built-in distro and a distro defined with repos look the same to the
// stages after it.
package expand

import (
	"cmp"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/internal/deb"
	"github.com/packtrix/packtrix/internal/pgp"
	"github.com/packtrix/packtrix/pkg/apis/packtrix"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/validation"
)

// Result is a manifest, expanded.
type Result struct {
	Targets []Target `json:"targets"`
}

// Target is a target with everything the later stages need.
type Target struct {
	Name   string            `json:"name"`
	Format packtrix.RepoType `json:"format"`
	// Distro is the catalog entry of a built-in distro, and Codename its
	// release's codename, such as "noble".
	Distro   string `json:"distro,omitempty"`
	Codename string `json:"codename,omitempty"`
	// Baseline is where minimal baselines come from, for a built-in distro.
	Baseline      *catalog.Baseline `json:"baseline,omitempty"`
	Architectures []Architecture    `json:"architectures"`
	Packages      []Package         `json:"packages"`
}

// Architecture holds the repositories of a target for one architecture.
type Architecture struct {
	Name         string       `json:"name"`
	Repositories []Repository `json:"repositories"`
}

// Repository is an upstream repository, ready to fetch.
type Repository struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Suite and Components are those of an APT repository. A flat
	// repository has a suite ending in "/" and no components.
	Suite      string   `json:"suite,omitempty"`
	Components []string `json:"components,omitempty"`
	// Keys are the fingerprints of the keys that may sign the repository.
	Keys []string `json:"keys"`
	// Extra is set for the repositories of extraRepos, which may come from
	// other vendors than the distro.
	Extra bool           `json:"extra,omitempty"`
	Auth  *packtrix.Auth `json:"-"`

	keyring *pgp.Keyring
}

// Keyring returns the keys that may sign the repository.
func (r Repository) Keyring() *pgp.Keyring {
	return r.keyring
}

// Package is a package of a target, by its name in the target's format.
type Package struct {
	Name string `json:"name"`
	// Label is the manifest's name for the package, when the format has its
	// own name for it.
	Label   string `json:"label,omitempty"`
	Version string `json:"version,omitempty"`
}

// nativeArchitectures are the names that RPM repositories use.
var nativeArchitectures = map[string]string{
	packtrix.ArchAMD64: "x86_64",
	packtrix.ArchARM64: "aarch64",
}

// native returns the name of an architecture in a package format.
func native(format packtrix.RepoType, arch string) string {
	if format == packtrix.RepoTypeRPM {
		return nativeArchitectures[arch]
	}
	return arch
}

// Expand expands a valid manifest with a catalog. dir is the manifest's
// directory, which relative key paths start from. Problems that need the
// catalog, such as an unknown distro, are returned like the manifest's own
// validation errors.
func Expand(m *packtrix.OfflineRepository, c *catalog.Catalog, dir string) (*Result, error) {
	e := &expander{catalog: c, dir: dir}
	res := &Result{}
	for i, t := range m.Spec.Targets {
		if target, ok := e.target(fmt.Sprintf("spec.targets[%d]", i), t); ok {
			res.Targets = append(res.Targets, target)
		}
	}
	e.checkPackages(m.Spec.Packages, res.Targets)
	for i := range res.Targets {
		res.Targets[i].Packages = packages(m.Spec.Packages, res.Targets[i])
	}
	if len(e.errs) > 0 {
		return nil, e.errs
	}
	return res, nil
}

type expander struct {
	catalog *catalog.Catalog
	dir     string
	errs    validation.ValidationError
}

func (e *expander) add(field, format string, args ...any) {
	e.errs = append(e.errs, validation.FieldError{Field: field, Detail: fmt.Sprintf(format, args...)})
}

func (e *expander) target(field string, t packtrix.Target) (Target, bool) {
	out := Target{Name: t.Name}
	var d *catalog.Distro
	if t.Distro != nil {
		var err error
		if d, err = e.catalog.Distro(t.Distro.Name); err != nil {
			e.add(field+".distro.name", "%v", err)
			return out, false
		}
		out.Format, out.Distro, out.Codename, out.Baseline = d.Format, d.Name, d.Release.Codename, &d.Baseline
	} else {
		out.Format = t.Repos[0].Type
	}

	components := e.components(field+".distro.components", t, d)
	extra := e.manifestRepositories(field+".extraRepos", t.ExtraRepos, out.Format)
	for i := range extra {
		extra[i].Extra = true
	}
	repos := e.manifestRepositories(field+".repos", t.Repos, out.Format)
	for j, arch := range t.Architectures {
		if d != nil && !slices.Contains(d.Architectures, arch) {
			e.add(fmt.Sprintf("%s.architectures[%d]", field, j), "%s doesn't support %s (supported: %s)", d.Name, arch, strings.Join(d.Architectures, ", "))
			continue
		}
		a := Architecture{Name: arch}
		if d != nil {
			a.Repositories = e.distroRepositories(field, t.Distro, d, arch, components)
		}
		for _, r := range slices.Concat(repos, extra) {
			r.URL = strings.ReplaceAll(r.URL, "{arch}", native(out.Format, arch))
			a.Repositories = append(a.Repositories, r)
		}
		out.Architectures = append(out.Architectures, a)
	}
	return out, true
}

// components returns the components that a distro target uses: the ones it
// chooses, or the distro's defaults.
func (e *expander) components(field string, t packtrix.Target, d *catalog.Distro) []string {
	if d == nil {
		return nil
	}
	if len(t.Distro.Components) == 0 {
		return d.DefaultComponents()
	}
	var known []string
	for _, c := range d.Components {
		known = append(known, c.Name)
	}
	for i, c := range t.Distro.Components {
		if !slices.Contains(known, c) {
			e.add(fmt.Sprintf("%s[%d]", field, i), "%s has no component %q (components: %s)", d.Name, c, strings.Join(known, ", "))
		}
	}
	return t.Distro.Components
}

// distroRepositories returns the repositories of a built-in distro for one
// architecture.
func (e *expander) distroRepositories(field string, td *packtrix.Distro, d *catalog.Distro, arch string, components []string) []Repository {
	var out []Repository
	for _, r := range d.Repositories {
		keyring, err := d.Keyring(r)
		if err != nil {
			// The catalog checks its keys when it loads.
			e.add(field+".distro.name", "catalog keys of %s: %v", d.Name, err)
			continue
		}
		repo := Repository{Name: r.Name, Suite: r.Suite, Keys: keyring.Fingerprints(), Auth: td.Auth, keyring: keyring}
		if d.Format == packtrix.RepoTypeAPT {
			repo.URL, repo.Components = mirrored(td.Mirror, r.URLFor(arch)), components
			out = append(out, repo)
			continue
		}
		// An RPM repository per component.
		for _, name := range components {
			c := d.Components[slices.IndexFunc(d.Components, func(c catalog.Component) bool { return c.Name == name })]
			repo.Name = r.Name + "-" + c.Name
			u := strings.NewReplacer("{component}", cmp.Or(c.Path, c.Name), "{arch}", native(d.Format, arch)).Replace(r.URLFor(arch))
			repo.URL = mirrored(td.Mirror, u)
			out = append(out, repo)
		}
	}
	return out
}

// mirrored moves an official URL under a mirror, if there is one, keeping
// its path: with the mirror https://nexus.internal/repository/, Debian's
// https://security.debian.org/debian-security/ becomes
// https://nexus.internal/repository/debian-security/. One mirror can then
// serve the distro's archives that live on separate servers, such as
// Ubuntu's ports or Debian's security archive.
func mirrored(mirror, official string) string {
	if mirror == "" {
		return official
	}
	u, err := url.Parse(official)
	if err != nil {
		return official
	}
	joined, err := url.JoinPath(mirror, u.Path)
	if err != nil {
		return official
	}
	return joined
}

// manifestRepositories returns the repositories a manifest defines, with
// their keys read from their gpgKey files.
func (e *expander) manifestRepositories(field string, repos []packtrix.Repository, format packtrix.RepoType) []Repository {
	var out []Repository
	for i, r := range repos {
		f := fmt.Sprintf("%s[%d]", field, i)
		if r.Type != format {
			e.add(f+".type", "the target's repositories are %s, not %s", format, r.Type)
			continue
		}
		keyring, err := e.keyring(r.GPGKey)
		if err != nil {
			e.add(f+".gpgKey", "%v", err)
			continue
		}
		out = append(out, Repository{
			Name: r.Name, URL: r.URL, Suite: r.Suite, Components: r.Components,
			Keys: keyring.Fingerprints(), Auth: r.Auth, keyring: keyring,
		})
	}
	return out
}

func (e *expander) keyring(path string) (*pgp.Keyring, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(e.dir, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return pgp.NewKeyring(data)
}

// checkPackages checks what only the catalog knows: vendor repositories,
// and version constraints in the format of the targets they apply to.
func (e *expander) checkPackages(pkgs []packtrix.Package, targets []Target) {
	for i, p := range pkgs {
		f := fmt.Sprintf("spec.packages[%d]", i)
		if p.Repo != "" {
			e.add(f+".repo", "unknown vendor repository %q: the catalog has no vendor repositories yet", p.Repo)
		}
		apt := slices.ContainsFunc(targets, func(t Target) bool { return t.Format == packtrix.RepoTypeAPT && applies(p, t) })
		if apt {
			if _, err := deb.ParseConstraint(string(p.Version)); err != nil {
				e.add(f+".version", "%v", err)
			}
		}
	}
}

// packages returns the packages that apply to a target, by their names in
// the target's format.
func packages(pkgs []packtrix.Package, t Target) []Package {
	var out []Package
	for _, p := range pkgs {
		if !applies(p, t) {
			continue
		}
		name := p.Name
		switch {
		case t.Format == packtrix.RepoTypeAPT && p.Deb != "":
			name = p.Deb
		case t.Format == packtrix.RepoTypeRPM && p.RPM != "":
			name = p.RPM
		}
		pkg := Package{Name: name, Version: string(p.Version)}
		if name != p.Name {
			pkg.Label = p.Name
		}
		out = append(out, pkg)
	}
	return out
}

// selectors name every target of a format in only and except.
var selectors = map[packtrix.RepoType]string{
	packtrix.RepoTypeAPT: packtrix.SelectorDeb,
	packtrix.RepoTypeRPM: packtrix.SelectorRPM,
}

// applies reports whether a package's only and except select a target.
func applies(p packtrix.Package, t Target) bool {
	selects := func(list []string) bool {
		return slices.Contains(list, t.Name) || slices.Contains(list, selectors[t.Format])
	}
	switch {
	case len(p.Only) > 0:
		return selects(p.Only)
	case len(p.Except) > 0:
		return !selects(p.Except)
	default:
		return true
	}
}
