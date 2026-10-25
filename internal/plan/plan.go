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

// Package plan works out what will cross the air gap: for each target and
// architecture, the packages to download, from which repository, and why.
// It reads and verifies the upstream metadata, but downloads no package.
package plan

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/internal/deb"
	"github.com/packtrix/packtrix/internal/expand"
	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/pool"
	repoapt "github.com/packtrix/packtrix/internal/repo/apt"
	"github.com/packtrix/packtrix/internal/resolve"
	aptmodel "github.com/packtrix/packtrix/internal/resolve/apt"
	"github.com/packtrix/packtrix/pkg/apis/packtrix"
)

// Plan is what each target and architecture needs.
type Plan struct {
	Targets []Target `json:"targets"`
}

// Target is the plan of one target and architecture.
type Target struct {
	Name         string            `json:"name"`
	Architecture string            `json:"architecture"`
	Format       packtrix.RepoType `json:"format"`
	// Distribution is what the repositories' Release files say, such as
	// "Ubuntu noble".
	Distribution string       `json:"distribution"`
	Repositories []Repository `json:"repositories"`
	// Packages are the packages to download: the listed ones first, then
	// their dependencies.
	Packages []Package `json:"packages"`
	// Excluded are the excluded packages that packages of the plan need.
	Excluded []Exclusion `json:"excluded,omitempty"`
}

// Repository is an upstream repository, as it was read.
type Repository struct {
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	Suite      string   `json:"suite"`
	Components []string `json:"components,omitempty"`
	// Release is the digest of its verified Release file.
	Release string `json:"release"`
	// Signer is the fingerprint of the key that signed its Release.
	Signer string `json:"signer"`
	// Packages is the number of packages its indexes list.
	Packages int  `json:"packages"`
	Extra    bool `json:"extra,omitempty"`
}

// Package is a package to download.
type Package struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Architecture string `json:"architecture"`
	// Repository is the name of the repository it comes from.
	Repository string `json:"repository"`
	URL        string `json:"url"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	// Why is the chain of packages that need it, up to the listed one,
	// such as [libcurl4t64, curl]. It is empty for a listed package.
	Why []string `json:"why,omitempty"`
}

// Exclusion is an excluded package and the packages that need it.
type Exclusion struct {
	Name     string   `json:"name"`
	NeededBy []string `json:"neededBy"`
}

// Lock returns the lock of a plan, for the manifest of this name and
// digest and the catalog of this version.
func (p *Plan) Lock(name, manifestDigest, catalogVersion string) *packtrix.Lock {
	l := &packtrix.Lock{Spec: packtrix.LockSpec{Manifest: manifestDigest, Catalog: catalogVersion}}
	l.Name = name
	for _, t := range p.Targets {
		lt := packtrix.LockTarget{Name: t.Name, Architecture: t.Architecture, Format: t.Format, Distribution: t.Distribution}
		for _, r := range t.Repositories {
			lt.Repositories = append(lt.Repositories, packtrix.LockRepository{
				Name: r.Name, URL: r.URL, Suite: r.Suite, Components: r.Components, Release: r.Release, Signer: r.Signer,
			})
		}
		for _, pkg := range t.Packages {
			lt.Packages = append(lt.Packages, packtrix.LockPackage{
				Name: pkg.Name, Version: pkg.Version, Architecture: pkg.Architecture, Repository: pkg.Repository,
				URL: pkg.URL, SHA256: pkg.SHA256, Size: pkg.Size, Why: pkg.Why,
			})
		}
		l.Spec.Targets = append(l.Spec.Targets, lt)
	}
	return l
}

// Size returns the total size of the target's packages.
func (t Target) Size() int64 {
	var n int64
	for _, p := range t.Packages {
		n += p.Size
	}
	return n
}

// Options configure a run.
type Options struct {
	// Dir is the manifest's directory, which relative paths start from.
	Dir string
	// Cache, if set, keeps the verified indexes across runs.
	Cache *pool.Pool
	// Reading, if set, is told about each repository before it is read.
	Reading func(target, arch string, r expand.Repository)
}

// Run plans a manifest: it expands it, reads and verifies the repositories
// of each target, and resolves each target and architecture. It plans
// every target it can, and returns the plan along with an error that lists
// every failure.
func Run(ctx context.Context, m *packtrix.OfflineRepository, c *catalog.Catalog, client *fetch.Client, opts Options) (*Plan, error) {
	expanded, err := expand.Expand(m, c, opts.Dir)
	if err != nil {
		return nil, err
	}
	p := &Plan{}
	var errs []error
	for _, t := range expanded.Targets {
		if t.Format != packtrix.RepoTypeAPT {
			errs = append(errs, fmt.Errorf("target %s: RPM targets aren't supported yet (planned for v0.2.0)", t.Name))
			continue
		}
		for _, a := range t.Architectures {
			pt, err := planTarget(ctx, m.Spec.Resolve, t, a, &repoapt.Reader{Client: client, Cache: opts.Cache}, opts)
			if err != nil {
				errs = append(errs, fmt.Errorf("target %s (%s): %w", t.Name, a.Name, err))
				continue
			}
			p.Targets = append(p.Targets, *pt)
		}
	}
	return p, errors.Join(errs...)
}

func planTarget(ctx context.Context, settings packtrix.Resolve, t expand.Target, a expand.Architecture, reader *repoapt.Reader, opts Options) (*Target, error) {
	out := &Target{Name: t.Name, Architecture: a.Name, Format: t.Format}

	// Read every repository, reporting every one that fails.
	var repos, base []*repoapt.Repository
	var errs []error
	for _, r := range a.Repositories {
		if opts.Reading != nil {
			opts.Reading(t.Name, a.Name, r)
		}
		auth, err := fetch.ResolveAuth(r.Auth, opts.Dir)
		if err != nil {
			errs = append(errs, fmt.Errorf("repository %s: auth: %w", r.Name, err))
			continue
		}
		src := repoapt.Source{Name: r.Name, URL: r.URL, Suite: r.Suite, Components: r.Components, Keyring: r.Keyring(), Auth: auth}
		repo, err := reader.Read(ctx, src, a.Name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		repos = append(repos, repo)
		if !r.Extra {
			base = append(base, repo)
		}
		out.Repositories = append(out.Repositories, Repository{
			Name: r.Name, URL: r.URL, Suite: r.Suite, Components: r.Components,
			Release: repo.Digest, Signer: repo.Signer, Packages: len(repo.Packages()), Extra: r.Extra,
		})
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	if err := repoapt.CheckSameRelease(base); err != nil {
		return nil, err
	}
	if len(base) > 0 {
		out.Distribution = base[0].Release.Distribution()
	}

	// Index every package, remembering its repository.
	var all []*deb.Package
	source := map[*deb.Package]*repoapt.Repository{}
	for _, repo := range repos {
		for _, p := range repo.Packages() {
			all = append(all, p)
			source[p] = repo
		}
	}
	ix := aptmodel.NewIndex(a.Name, all)

	baseline, err := baseline(settings.Baseline, t, a, base)
	if err != nil {
		return nil, err
	}

	var listed []*deb.Package
	for _, p := range t.Packages {
		c, err := deb.ParseConstraint(p.Version)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		pkg, err := ix.Select(p.Name, c)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		listed = append(listed, pkg)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	model := aptmodel.NewModel(ix, baseline, aptmodel.Options{
		Recommends:      settings.Recommends,
		AllAlternatives: settings.Alternatives == packtrix.AlternativesAll,
		Exclude:         settings.Exclude,
	})
	result, err := resolve.Closure(model, listed)
	if err != nil {
		return nil, err
	}

	for _, p := range result.Packages {
		repo := source[p]
		out.Packages = append(out.Packages, Package{
			Name:         p.Name,
			Version:      p.Version.String(),
			Architecture: p.Architecture,
			Repository:   repo.Name,
			URL:          strings.TrimSuffix(repo.URL, "/") + "/" + p.Filename,
			Size:         p.Size,
			SHA256:       p.SHA256,
			Why:          why(result, p),
		})
	}
	for _, e := range model.Excluded() {
		x := Exclusion{Name: e.Name}
		for _, p := range e.NeededBy {
			x.NeededBy = append(x.NeededBy, p.Name)
		}
		out.Excluded = append(out.Excluded, x)
	}
	return out, nil
}

// baseline returns what the machines already have, for the baseline
// setting. A minimal baseline comes from the release repository's main
// component, as the catalog says.
func baseline(setting string, t expand.Target, a expand.Architecture, base []*repoapt.Repository) ([]*deb.Package, error) {
	var release []*deb.Package
	if setting == packtrix.BaselineMinimal {
		if t.Baseline == nil {
			return nil, errors.New(`baseline "minimal" needs a built-in distro; set resolve.baseline to "none" for a target defined with repos`)
		}
		for _, repo := range base {
			if repo.Name != t.Baseline.Repository {
				continue
			}
			for _, ix := range repo.Indexes {
				if ix.Component == t.Baseline.Component {
					release = append(release, ix.Packages...)
				}
			}
		}
		if len(release) == 0 {
			return nil, fmt.Errorf("the minimal baseline comes from component %s of repository %s, which the target doesn't use", t.Baseline.Component, t.Baseline.Repository)
		}
	}
	return aptmodel.Baseline(setting, a.Name, release)
}

// why returns the names of the packages that brought p in, up to the listed
// one.
func why(result *resolve.Result[*deb.Package], p *deb.Package) []string {
	var chain []string
	for {
		reason, _ := result.Reason(p)
		if reason.By == nil {
			return chain
		}
		p = *reason.By
		chain = append(chain, p.Name)
	}
}
