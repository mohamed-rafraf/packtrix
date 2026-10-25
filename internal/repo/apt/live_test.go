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
)

func TestLiveRead(t *testing.T) {
	if testing.Short() {
		t.Skip("downloads indexes from the Ubuntu and Debian archives")
	}
	c, err := catalog.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	client, err := fetch.New(fetch.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		distro, repository, want string
	}{
		{"ubuntu-24.04", "release", "libc6"},
		// Only xz indexes, and a binary-all index.
		{"debian-12", "security", "linux-image-amd64"},
	} {
		t.Run(tt.distro+"/"+tt.repository, func(t *testing.T) {
			d, err := c.Distro(tt.distro)
			if err != nil {
				t.Fatal(err)
			}
			i := slices.IndexFunc(d.Repositories, func(r catalog.Repository) bool { return r.Name == tt.repository })
			r := d.Repositories[i]
			keyring, err := d.Keyring(r)
			if err != nil {
				t.Fatal(err)
			}
			src := Source{Name: r.Name, URL: r.URLFor("amd64"), Suite: r.Suite, Components: []string{"main"}, Keyring: keyring}
			repo, err := Read(t.Context(), client, src, "amd64")
			if err != nil {
				t.Fatal(err)
			}
			var archs []string
			for _, ix := range repo.Indexes {
				archs = append(archs, ix.Architecture)
			}
			t.Logf("%s: %d packages from indexes %q, signed by %s", repo.Release.Distribution(), len(repo.Packages()), archs, repo.Signer)
			if !slices.ContainsFunc(repo.Packages(), func(p *deb.Package) bool { return p.Name == tt.want && p.Filename != "" && p.Size > 0 }) {
				t.Errorf("no %s with a filename and a size", tt.want)
			}
			if !slices.Contains(keyring.Fingerprints(), repo.Signer) {
				t.Errorf("signed by %s, which the catalog doesn't pin", repo.Signer)
			}
		})
	}
}
