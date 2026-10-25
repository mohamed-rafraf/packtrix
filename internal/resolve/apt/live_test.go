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

package apt

import (
	"slices"
	"testing"

	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/internal/deb"
	"github.com/packtrix/packtrix/internal/fetch"
	repoapt "github.com/packtrix/packtrix/internal/repo/apt"
	"github.com/packtrix/packtrix/internal/resolve"
)

// read reads and verifies a repository of a built-in distro, with the
// main and universe components.
func read(t *testing.T, client *fetch.Client, d *catalog.Distro, r catalog.Repository) *repoapt.Repository {
	t.Helper()
	keyring, err := d.Keyring(r)
	if err != nil {
		t.Fatal(err)
	}
	src := repoapt.Source{Name: r.Name, URL: r.URLFor("amd64"), Suite: r.Suite, Components: []string{"main", "universe"}, Keyring: keyring}
	repo, err := (&repoapt.Reader{Client: client}).Read(t.Context(), src, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestLiveUbuntu2404(t *testing.T) {
	if testing.Short() {
		t.Skip("downloads the Ubuntu 24.04 indexes")
	}
	c, err := catalog.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	d, err := c.Distro("ubuntu-24.04")
	if err != nil {
		t.Fatal(err)
	}
	client, err := fetch.New(fetch.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Security first, then updates, then the release, whose main component
	// the minimal baseline comes from.
	var packages, release []*deb.Package
	for _, name := range []string{"security", "updates", "release"} {
		i := slices.IndexFunc(d.Repositories, func(r catalog.Repository) bool { return r.Name == name })
		repo := read(t, client, d, d.Repositories[i])
		packages = append(packages, repo.Packages()...)
		if name == d.Baseline.Repository {
			for _, ix := range repo.Indexes {
				if ix.Component == d.Baseline.Component {
					release = append(release, ix.Packages...)
				}
			}
		}
	}
	ix := NewIndex("amd64", packages)

	minimal, err := MinimalBaseline("amd64", release)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("minimal baseline: %d packages", len(minimal))
	inMinimal := map[string]bool{}
	for _, p := range minimal {
		inMinimal[p.String()] = true
	}
	// The noble suite never changes, so neither do its versions.
	for _, want := range []string{"libc6=2.39-0ubuntu8", "dpkg=1.22.6ubuntu6", "apt=2.7.14build2"} {
		if !inMinimal[want] {
			t.Errorf("the minimal baseline doesn't contain %s", want)
		}
	}

	var listed []*deb.Package
	for _, name := range []string{"curl", "nginx", "docker.io"} {
		listed = append(listed, pick(t, ix, name))
	}
	for _, tt := range []struct {
		baseline string
		packages []*deb.Package
		want     []string
	}{
		{"none", nil, []string{"libcurl4t64", "libc6", "nginx-common", "containerd", "runc", "iptables"}},
		{"minimal", minimal, []string{"libcurl4t64", "nginx-common", "containerd", "runc", "iptables"}},
	} {
		t.Run(tt.baseline, func(t *testing.T) {
			result, err := resolve.Closure(NewModel(ix, tt.packages, Options{}), listed)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%d packages from %d in the indexes", len(result.Packages), len(packages))
			found := map[string]*deb.Package{}
			for _, p := range result.Packages {
				if found[p.Name] != nil {
					t.Errorf("two versions of %s: %s and %s", p.Name, found[p.Name], p)
				}
				found[p.Name] = p
				if inMinimal[p.String()] && tt.baseline == "minimal" {
					t.Errorf("%s is in the baseline", p)
				}
			}
			for _, name := range tt.want {
				if p := found[name]; p == nil {
					t.Errorf("the closure doesn't contain %s", name)
				} else {
					t.Log(result.Why(p))
				}
			}
		})
	}
}
