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

// Package catalog is Packtrix's built-in knowledge of distributions: where
// their repositories are, which components they have, and the keys that sign
// them, pinned by fingerprint. It lets a manifest say "distro: ubuntu-24.04".
//
// The data is embedded in the binary: catalog.yaml holds the catalog's
// version, distros/<name>.yaml one entry each, and keys/<name>.asc the keys.
package catalog

import (
	"cmp"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"sigs.k8s.io/yaml"

	"github.com/packtrix/packtrix/internal/pgp"
	"github.com/packtrix/packtrix/pkg/apis/packtrix"
)

//go:embed catalog.yaml distros keys
var builtin embed.FS

// Builtin returns the catalog embedded in the binary.
var Builtin = sync.OnceValues(func() (*Catalog, error) {
	return Load(builtin)
})

// Catalog holds the distributions that targets can name.
type Catalog struct {
	// Version changes whenever an entry or a key changes. The lockfile
	// records it.
	Version string
	distros map[string]*Distro
}

// Distro is a distribution release, such as ubuntu-24.04.
type Distro struct {
	Name string `json:"name"`
	// Family is "ubuntu", "debian" or "el".
	Family string `json:"family"`
	// Format is the package format of the repositories.
	Format        packtrix.RepoType `json:"format"`
	Release       Release           `json:"release"`
	Architectures []string          `json:"architectures"`
	Components    []Component       `json:"components"`
	Repositories  []Repository      `json:"repositories"`
	// Baseline is where minimal baselines come from: the release, as it
	// shipped, without its updates.
	Baseline Baseline `json:"baseline"`
	// Keys are the fingerprints of the keys that sign the repositories, by
	// key name. The keys are in keys/<name>.asc.
	Keys map[string]string `json:"keys"`

	keyData map[string][]byte
}

// Release identifies the release of a distribution.
type Release struct {
	Version string `json:"version"`
	// Codename is the APT name of the release, such as "noble".
	Codename string `json:"codename,omitempty"`
}

// Component is a part of a distribution, such as Ubuntu's universe or EL's
// crb.
type Component struct {
	Name string `json:"name"`
	// Default components are used when a target doesn't choose.
	Default bool `json:"default,omitempty"`
	// Path is the vendor's name for the component in RPM repository URLs,
	// such as "PowerTools" for crb on EL8. Empty means Name.
	Path string `json:"path,omitempty"`
}

// Repository is a repository of a distribution, such as Ubuntu's
// noble-security.
type Repository struct {
	Name string `json:"name"`
	// URL is the base URL. In RPM repositories, {component} is replaced with
	// the component's path and {arch} with the native architecture.
	URL string `json:"url"`
	// ArchURLs replace URL for some architectures, such as
	// ports.ubuntu.com for Ubuntu's arm64.
	ArchURLs map[string]string `json:"archURLs,omitempty"`
	// Suite is the APT suite, such as "noble-updates".
	Suite string `json:"suite,omitempty"`
	// Keys name the keys that may sign the repository.
	Keys []string `json:"keys"`
}

// Baseline names the repository and component that minimal baselines come
// from.
type Baseline struct {
	Repository string `json:"repository"`
	Component  string `json:"component"`
}

// URLFor returns the repository's base URL for an architecture.
func (r Repository) URLFor(arch string) string {
	if u, ok := r.ArchURLs[arch]; ok {
		return u
	}
	return r.URL
}

// DefaultComponents returns the names of the components that targets use
// unless they choose.
func (d *Distro) DefaultComponents() []string {
	var names []string
	for _, c := range d.Components {
		if c.Default {
			names = append(names, c.Name)
		}
	}
	return names
}

// Keyring returns the keys that may sign a repository, pinned by
// fingerprint.
func (d *Distro) Keyring(r Repository) (*pgp.Keyring, error) {
	var data []byte
	var fingerprints []string
	for _, name := range r.Keys {
		data = append(data, d.keyData[name]...)
		fingerprints = append(fingerprints, d.Keys[name])
	}
	return pgp.NewKeyring(data, fingerprints...)
}

// Distros returns every distribution, by family, then by release.
func (c *Catalog) Distros() []*Distro {
	var out []*Distro
	for _, d := range c.distros {
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b *Distro) int {
		return cmp.Or(strings.Compare(a.Family, b.Family), compareVersions(a.Release.Version, b.Release.Version))
	})
	return out
}

// compareVersions compares release versions such as "22.04" and "24.04", or
// "9" and "10", number by number.
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := range min(len(as), len(bs)) {
		x, _ := strconv.Atoi(as[i])
		y, _ := strconv.Atoi(bs[i])
		if c := cmp.Compare(x, y); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(as), len(bs))
}

// Distro returns the named distribution. For an unknown name, the error
// suggests the closest known one.
func (c *Catalog) Distro(name string) (*Distro, error) {
	if d, ok := c.distros[name]; ok {
		return d, nil
	}
	var names []string
	for n := range c.distros {
		names = append(names, n)
	}
	slices.Sort(names)
	if s := closest(name, names); s != "" {
		return nil, fmt.Errorf("unknown distro %q; did you mean %q?", name, s)
	}
	return nil, fmt.Errorf("unknown distro %q (known: %s)", name, strings.Join(names, ", "))
}

// closest returns the candidate within a few edits of name, or "".
func closest(name string, candidates []string) string {
	best, bestDistance := "", max(2, len(name)/4)+1
	for _, c := range candidates {
		if d := distance(name, c); d < bestDistance {
			best, bestDistance = c, d
		}
	}
	return best
}

// distance is the Levenshtein distance between a and b.
func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// Load reads and checks a catalog: catalog.yaml, distros/*.yaml and
// keys/*.asc. Every key must match the fingerprint its entries pin.
func Load(fsys fs.FS) (*Catalog, error) {
	data, err := fs.ReadFile(fsys, "catalog.yaml")
	if err != nil {
		return nil, err
	}
	var meta struct {
		Version string `json:"version"`
	}
	if err := yaml.UnmarshalStrict(data, &meta); err != nil {
		return nil, fmt.Errorf("catalog.yaml: %w", err)
	}
	if meta.Version == "" {
		return nil, errors.New("catalog.yaml: version: required")
	}
	c := &Catalog{Version: meta.Version, distros: map[string]*Distro{}}

	files, err := fs.Glob(fsys, "distros/*.yaml")
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, file := range files {
		d, err := loadDistro(fsys, file)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", file, err))
			continue
		}
		c.distros[d.Name] = d
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return c, nil
}

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)

// formats are the package formats of each family.
var formats = map[string]packtrix.RepoType{
	"ubuntu": packtrix.RepoTypeAPT,
	"debian": packtrix.RepoTypeAPT,
	"el":     packtrix.RepoTypeRPM,
}

func loadDistro(fsys fs.FS, file string) (*Distro, error) {
	data, err := fs.ReadFile(fsys, file)
	if err != nil {
		return nil, err
	}
	d := &Distro{}
	if err := yaml.UnmarshalStrict(data, d); err != nil {
		return nil, err
	}
	var p problems
	d.checkRelease(&p, strings.TrimSuffix(path.Base(file), ".yaml"))
	d.checkComponents(&p)
	d.checkRepositories(&p)
	d.loadKeys(&p, fsys)
	if len(p) > 0 {
		return nil, errors.New(strings.Join(p, "; "))
	}
	return d, nil
}

// problems collects what is wrong with an entry.
type problems []string

func (p *problems) add(format string, args ...any) {
	*p = append(*p, fmt.Sprintf(format, args...))
}

func (d *Distro) checkRelease(p *problems, fileName string) {
	switch {
	case d.Name != fileName:
		p.add("name: %q doesn't match the file name", d.Name)
	case !nameRE.MatchString(d.Name):
		p.add("name: invalid name %q", d.Name)
	}
	if format, ok := formats[d.Family]; !ok {
		p.add("family: %q isn't one of ubuntu, debian, el", d.Family)
	} else if d.Format != format {
		p.add("format: %s distributions use %s, not %q", d.Family, format, d.Format)
	}
	if d.Release.Version == "" {
		p.add("release.version: required")
	}
	if d.Format == packtrix.RepoTypeAPT && d.Release.Codename == "" {
		p.add("release.codename: required for APT distributions")
	}
	if len(d.Architectures) == 0 {
		p.add("architectures: required")
	}
	for i, a := range d.Architectures {
		if !slices.Contains(packtrix.SupportedArchitectures, a) || slices.Contains(d.Architectures[:i], a) {
			p.add("architectures[%d]: unsupported or duplicate architecture %q", i, a)
		}
	}
}

func (d *Distro) checkComponents(p *problems) {
	if len(d.DefaultComponents()) == 0 {
		p.add("components: at least one must be a default")
	}
	var names []string
	for i, c := range d.Components {
		if !nameRE.MatchString(c.Name) || slices.Contains(names, c.Name) {
			p.add("components[%d].name: invalid or duplicate name %q", i, c.Name)
		}
		if d.Format == packtrix.RepoTypeAPT && c.Path != "" {
			p.add("components[%d].path: only RPM components have a path", i)
		}
		names = append(names, c.Name)
	}
	if !slices.Contains(names, d.Baseline.Component) {
		p.add("baseline.component: unknown component %q", d.Baseline.Component)
	}
}

func (d *Distro) checkRepositories(p *problems) {
	apt := d.Format == packtrix.RepoTypeAPT
	if len(d.Repositories) == 0 {
		p.add("repositories: required")
	}
	var names []string
	for i, r := range d.Repositories {
		f := fmt.Sprintf("repositories[%d]", i)
		if !nameRE.MatchString(r.Name) || slices.Contains(names, r.Name) {
			p.add("%s.name: invalid or duplicate name %q", f, r.Name)
		}
		names = append(names, r.Name)
		switch {
		case apt && r.Suite == "":
			p.add("%s.suite: required for APT repositories", f)
		case !apt && r.Suite != "":
			p.add("%s.suite: only APT repositories have a suite", f)
		}
		for _, u := range append([]string{r.URL}, mapValues(r.ArchURLs)...) {
			if err := checkURL(u, apt); err != nil {
				p.add("%s: %v", f, err)
			}
		}
		for _, a := range mapKeys(r.ArchURLs) {
			if !slices.Contains(d.Architectures, a) {
				p.add("%s.archURLs: %q isn't an architecture of the distribution", f, a)
			}
		}
		if len(r.Keys) == 0 {
			p.add("%s.keys: required", f)
		}
		for _, k := range r.Keys {
			if _, ok := d.Keys[k]; !ok {
				p.add("%s.keys: unknown key %q", f, k)
			}
		}
	}
	if !slices.Contains(names, d.Baseline.Repository) {
		p.add("baseline.repository: unknown repository %q", d.Baseline.Repository)
	}
}

// loadKeys reads the entry's keys, each of which must contain the key its
// fingerprint pins.
func (d *Distro) loadKeys(p *problems, fsys fs.FS) {
	d.keyData = map[string][]byte{}
	for _, name := range mapKeys(d.Keys) {
		data, err := fs.ReadFile(fsys, "keys/"+name+".asc")
		if err != nil {
			p.add("keys.%s: %v", name, err)
			continue
		}
		if _, err := pgp.NewKeyring(data, d.Keys[name]); err != nil {
			p.add("keys.%s: %v", name, err)
			continue
		}
		d.keyData[name] = data
	}
}

// checkURL checks a repository's base URL: HTTPS, ending with "/", with
// placeholders only in RPM repositories.
func checkURL(raw string, apt bool) error {
	u, err := url.Parse(raw)
	switch {
	case err != nil:
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	case u.Scheme != "https" || u.Host == "":
		return fmt.Errorf("URL %q must be https://", raw)
	case !strings.HasSuffix(u.Path, "/"):
		return fmt.Errorf("URL %q must end with /", raw)
	}
	rest := strings.NewReplacer("{arch}", "", "{component}", "").Replace(raw)
	switch {
	case strings.ContainsAny(rest, "{}"):
		return fmt.Errorf("URL %q: only {arch} and {component} placeholders are supported", raw)
	case apt && rest != raw:
		return fmt.Errorf("URL %q: APT repositories have no placeholders; use archURLs", raw)
	}
	return nil
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func mapValues(m map[string]string) []string {
	var values []string
	for _, k := range mapKeys(m) {
		values = append(values, m[k])
	}
	return values
}
