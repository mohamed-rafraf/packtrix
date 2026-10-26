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

package build

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/internal/download"
	"github.com/packtrix/packtrix/internal/expand"
	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/plan"
	"github.com/packtrix/packtrix/internal/pool"
	repoapt "github.com/packtrix/packtrix/internal/repo/apt"
	"github.com/packtrix/packtrix/internal/repo/apt/apttest"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
)

// TestLiveAPTGet builds a repository of curl for Ubuntu 24.04 from the real
// archive, and has apt-get, when it is installed, read it with only the
// published key trusted and download curl from it.
func TestLiveAPTGet(t *testing.T) {
	if testing.Short() {
		t.Skip("downloads from the Ubuntu archive")
	}
	m, err := manifest.Parse([]byte("apiVersion: packtrix.io/v1alpha1\nkind: OfflineRepository\nmetadata:\n  name: live\nspec:\n  targets:\n    - distro: ubuntu-24.04\n  packages: [curl]\n"))
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
	cache, err := pool.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	packages, err := pool.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	expanded, err := expand.Expand(m, c, "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := plan.Run(t.Context(), m, c, client, plan.Options{Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	l := p.Lock(m.Name, "sha256:"+strings.Repeat("0", 64), c.Version)
	if _, err := download.Packages(t.Context(), client, packages, l, download.Options{}); err != nil {
		t.Fatal(err)
	}
	signer, _ := apttest.Key(t)
	dir := t.TempDir()
	targets, err := APT(t.Context(), l, expanded, dir, Options{Reader: &repoapt.Reader{Client: client, Cache: cache}, Packages: packages, Signer: signer})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", targets)

	server := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer server.Close()

	// apt-get trusts only the published key, and uses the generated source
	// entry, with the key where the test can put it.
	if err := Clients(dir, targets, signer, server.URL); err != nil {
		t.Fatal(err)
	}
	sources, err := os.ReadFile(filepath.Join(dir, "clients", "ubuntu-24.04.sources"))
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "keys", KeyName+".gpg")
	apt := apttest.NewAPT(t, strings.Replace(string(sources), "Signed-By: "+keyPath, "Signed-By: "+key, 1))
	t.Log(apt.Run("update"))
	t.Log(apt.Run("download", "curl"))
	debs, _ := filepath.Glob(filepath.Join(apt.DownloadDir(), "curl_*.deb"))
	curl := l.Spec.Targets[0].Packages[0]
	if len(debs) != 1 {
		t.Fatalf("apt-get download wrote %q", debs)
	}
	data, err := os.ReadFile(debs[0])
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != curl.SHA256 {
		t.Errorf("apt-get downloaded a curl that doesn't match the lock")
	}
}
