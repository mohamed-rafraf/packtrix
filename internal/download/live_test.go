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

package download

import (
	"testing"

	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/plan"
	"github.com/packtrix/packtrix/internal/pool"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
)

// TestLiveDownload plans curl for Ubuntu 24.04 and downloads its packages
// from the real archive.
func TestLiveDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("downloads from the Ubuntu archive")
	}
	m, err := manifest.Parse([]byte(`apiVersion: packtrix.io/v1alpha1
kind: OfflineRepository
metadata:
  name: live
spec:
  targets:
    - distro: ubuntu-24.04
  packages: [curl]
`))
	if err != nil {
		t.Fatal(err)
	}
	c, err := catalog.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	client, err := fetch.New(fetch.Options{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := plan.Run(t.Context(), m, c, client, plan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	l := p.Lock(m.Name, "sha256:test", c.Version)
	packages, err := pool.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got, err := Packages(t.Context(), client, packages, l, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", got)
	for _, pkg := range l.Spec.Targets[0].Packages {
		if !packages.Has(pkg.SHA256) {
			t.Errorf("%s isn't in the pool", pkg.Name)
		}
	}
}
