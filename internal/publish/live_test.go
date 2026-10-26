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

package publish

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/plan"
	"github.com/packtrix/packtrix/internal/pool"
	"github.com/packtrix/packtrix/internal/repo/apt/apttest"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
)

// TestLiveSync plans and syncs curl and jq for Ubuntu 24.04 from the real
// archive, serves the published directory, and has apt-get, when it is
// installed, use it through the generated client files.
func TestLiveSync(t *testing.T) {
	if testing.Short() {
		t.Skip("downloads from the Ubuntu archive")
	}
	m, err := manifest.Parse([]byte("apiVersion: packtrix.io/v1alpha1\nkind: OfflineRepository\nmetadata:\n  name: live\nspec:\n  targets:\n    - distro: ubuntu-24.04\n  packages: [curl, jq]\n"))
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
	p, err := plan.Run(t.Context(), m, c, client, plan.Options{Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := manifest.Digest(m)
	if err != nil {
		t.Fatal(err)
	}
	signer, _ := apttest.Key(t)
	server := httptest.NewUnstartedServer(nil)
	opts := Options{
		Manifest: m, ManifestDir: t.TempDir(), Lock: p.Lock(m.Name, digest, c.Version), Catalog: c,
		Client: client, Cache: cache, Signer: signer, BaseURL: "http://" + server.Listener.Addr().String(),
	}
	res, err := Sync(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", res.Targets)
	server.Config.Handler = http.FileServer(http.Dir(res.Dir))
	server.Start()
	defer server.Close()

	// The generated source entry, with the key where the test can put it.
	sources, err := os.ReadFile(filepath.Join(res.Dir, "clients", "ubuntu-24.04.sources"))
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(res.Dir, "keys", "packtrix-repo.gpg")
	apt := apttest.NewAPT(t, strings.Replace(string(sources), "Signed-By: /etc/apt/keyrings/packtrix-repo.gpg", "Signed-By: "+key, 1))
	t.Log(apt.Run("update"))
	t.Log(apt.Run("download", "curl", "jq", "libjq1", "libonig5"))
	debs, _ := filepath.Glob(filepath.Join(apt.DownloadDir(), "*.deb"))
	if len(debs) != 4 {
		t.Errorf("apt-get download wrote %q", debs)
	}
}
