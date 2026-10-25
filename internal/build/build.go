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

// Package build writes the repositories that Packtrix publishes, from a
// lock: for each target, the locked packages from the package pool, with
// the upstream metadata of each, in one repository signed with the user's
// key.
package build

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/packtrix/packtrix/internal/expand"
	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/pgp"
	"github.com/packtrix/packtrix/internal/pool"
	repoapt "github.com/packtrix/packtrix/internal/repo/apt"
	"github.com/packtrix/packtrix/pkg/apis/packtrix"
)

// Origin is the Origin of the repositories Packtrix publishes.
const Origin = "Packtrix"

// Options configure a build.
type Options struct {
	// Reader reads the upstream metadata again, normally from its cache,
	// for each package's paragraph.
	Reader *repoapt.Reader
	// Packages is the package pool that holds the locked packages.
	Packages *pool.Pool
	// Signer signs the repositories.
	Signer *pgp.Signer
	// ManifestDir is the manifest's directory, which the paths of
	// credentials start from.
	ManifestDir string
}

// Target sums up the repository of a target.
type Target struct {
	Name string
	// Dir is where the repository is.
	Dir string
	// Codename is the suite that machines use, such as "noble".
	Codename      string
	Architectures []string
	// Files and Bytes count the package files.
	Files int
	Bytes int64
}

// APT writes the repository of each target of a lock into dir/apt/<target>.
// The lock and the expanded manifest must agree, as they do when the lock
// was planned from the manifest.
func APT(ctx context.Context, l *packtrix.Lock, expanded *expand.Result, dir string, opts Options) ([]Target, error) {
	var out []Target
	for _, name := range targetNames(l) {
		t, err := buildTarget(ctx, l, expanded, name, filepath.Join(dir, "apt", name), opts)
		if err != nil {
			return out, fmt.Errorf("target %s: %w", name, err)
		}
		out = append(out, *t)
	}
	return out, nil
}

func targetNames(l *packtrix.Lock) []string {
	var names []string
	for _, t := range l.Spec.Targets {
		if !slices.Contains(names, t.Name) {
			names = append(names, t.Name)
		}
	}
	slices.Sort(names)
	return names
}

func buildTarget(ctx context.Context, l *packtrix.Lock, expanded *expand.Result, name, dir string, opts Options) (*Target, error) {
	i := slices.IndexFunc(expanded.Targets, func(t expand.Target) bool { return t.Name == name })
	if i < 0 {
		return nil, fmt.Errorf("the manifest has no target %s; run packtrix plan", name)
	}
	et := expanded.Targets[i]
	suite := repoapt.Suite{Origin: Origin, Label: name}
	codename := et.Codename
	files := map[string]int64{}

	for _, lt := range l.Spec.Targets {
		if lt.Name != name {
			continue
		}
		if lt.Format != packtrix.RepoTypeAPT {
			return nil, fmt.Errorf("%s targets aren't supported yet", lt.Format)
		}
		suite.Architectures = append(suite.Architectures, lt.Architecture)
		j := slices.IndexFunc(et.Architectures, func(a expand.Architecture) bool { return a.Name == lt.Architecture })
		if j < 0 {
			return nil, fmt.Errorf("the manifest has no architecture %s; run packtrix plan", lt.Architecture)
		}
		for _, lr := range lt.Repositories {
			want := map[repoapt.Key]bool{}
			for _, p := range lt.Packages {
				if p.Repository == lr.Name {
					want[repoapt.Key{Name: p.Name, Version: p.Version, Architecture: p.Architecture}] = true
				}
			}
			if len(want) == 0 {
				continue
			}
			src, extra, err := source(et.Architectures[j], lr, opts.ManifestDir)
			if err != nil {
				return nil, err
			}
			found, rel, err := opts.Reader.Paragraphs(ctx, src, lt.Architecture, lr.Release, want)
			if err != nil {
				return nil, err
			}
			if codename == "" && !extra {
				codename, _, _ = strings.Cut(rel.Codename, "-")
			}
			if rel.Date.After(suite.Date) {
				suite.Date = rel.Date
			}
			for _, p := range lt.Packages {
				if p.Repository != lr.Name {
					continue
				}
				paragraph, ok := found[repoapt.Key{Name: p.Name, Version: p.Version, Architecture: p.Architecture}]
				if !ok {
					return nil, fmt.Errorf("package %s %s isn't in repository %s as plan read it", p.Name, p.Version, lr.Name)
				}
				if !opts.Packages.Has(p.SHA256) {
					return nil, fmt.Errorf("package %s %s isn't in the package pool; download it first", p.Name, p.Version)
				}
				filename := published(lr, p)
				suite.Packages = append(suite.Packages, repoapt.Published{
					Name: p.Name, Version: p.Version, Architecture: lt.Architecture,
					Paragraph: paragraph, Filename: filename, SHA256: p.SHA256,
				})
				files[filename] = p.Size
			}
		}
	}

	suite.Codename = cmpOr(codename, "stable")
	if err := repoapt.Write(dir, suite, opts.Packages, opts.Signer); err != nil {
		return nil, err
	}
	t := &Target{Name: name, Dir: dir, Codename: suite.Codename, Architectures: suite.Architectures, Files: len(files)}
	for _, size := range files {
		t.Bytes += size
	}
	return t, nil
}

// source returns how to read a lock's repository again, with the keys and
// the credentials that the manifest gives it, and whether it is one of the
// target's extraRepos.
func source(a expand.Architecture, lr packtrix.LockRepository, manifestDir string) (repoapt.Source, bool, error) {
	i := slices.IndexFunc(a.Repositories, func(r expand.Repository) bool { return r.Name == lr.Name })
	if i < 0 {
		return repoapt.Source{}, false, fmt.Errorf("the manifest has no repository %s; run packtrix plan", lr.Name)
	}
	r := a.Repositories[i]
	auth, err := fetch.ResolveAuth(r.Auth, manifestDir)
	if err != nil {
		return repoapt.Source{}, false, fmt.Errorf("repository %s: auth: %w", r.Name, err)
	}
	return repoapt.Source{
		Name: lr.Name, URL: lr.URL, Suite: lr.Suite, Components: lr.Components, Keyring: r.Keyring(), Auth: auth,
	}, r.Extra, nil
}

// published returns where a package goes in the published repository: its
// upstream path, under pool/<repository>/, so that two repositories can't
// collide.
func published(lr packtrix.LockRepository, p packtrix.LockPackage) string {
	rel, ok := strings.CutPrefix(p.URL, strings.TrimSuffix(lr.URL, "/")+"/")
	if !ok {
		rel = p.URL[strings.LastIndex(p.URL, "/")+1:]
	}
	return "pool/" + lr.Name + "/" + strings.TrimPrefix(rel, "pool/")
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
