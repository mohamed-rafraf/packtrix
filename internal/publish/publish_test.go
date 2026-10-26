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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/internal/download"
	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/plan"
	"github.com/packtrix/packtrix/internal/pool"
	"github.com/packtrix/packtrix/internal/repo/apt/apttest"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
)

// deb publishes a package file in the archive and returns its paragraph.
func deb(a *apttest.Archive, name, version, fields string) string {
	filename := fmt.Sprintf("pool/main/%s_%s_amd64.deb", name, version)
	content := []byte("the " + filename)
	a.Write("debian/"+filename, content)
	sum := sha256.Sum256(content)
	return fmt.Sprintf("Package: %s\nVersion: %s\nArchitecture: amd64\n%sFilename: %s\nSize: %d\nSHA256: %s\n\n",
		name, version, fields, filename, len(content), hex.EncodeToString(sum[:]))
}

const releaseFields = "Origin: Debian\nSuite: test\nCodename: test\nDate: Fri, 23 Oct 2026 10:00:00 UTC\nArchitectures: amd64\nComponents: main\n"

// newOptions returns the options of a sync of curl from a test distro, with
// its lock planned.
func newOptions(t *testing.T, keepSnapshots int) (Options, *apttest.Archive) {
	t.Helper()
	upstream, _ := apttest.Key(t)
	a := apttest.New(t, upstream, true)
	a.Publish(apttest.Suite{
		Dir: "debian/dists/test", Fields: releaseFields, Compressions: []string{".xz"},
		Indexes: map[string]string{"main/binary-amd64/Packages": deb(a, "curl", "8.1", "Depends: libcurl4\n") + deb(a, "libcurl4", "8.1", "")},
	})
	public, err := upstream.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	c, err := catalog.Load(fstest.MapFS{
		"catalog.yaml":  {Data: []byte("version: test\n")},
		"keys/test.asc": {Data: public},
		"distros/test-1.yaml": {Data: []byte(`
name: test-1
family: debian
format: apt
release: { version: "1", codename: test }
architectures: [amd64]
components: [{ name: main, default: true }]
repositories:
  - { name: release, url: ` + a.URL() + `/debian/, suite: test, keys: [test] }
baseline: { repository: release, component: main }
keys: { test: ` + upstream.Fingerprint() + ` }
`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	m, err := manifest.Parse([]byte(fmt.Sprintf(`apiVersion: packtrix.io/v1alpha1
kind: OfflineRepository
metadata:
  name: test
spec:
  targets:
    - distro: test-1
  packages: [curl]
  resolve:
    baseline: none
  publish:
    keepSnapshots: %d
`, keepSnapshots)))
	if err != nil {
		t.Fatal(err)
	}
	client, err := fetch.New(fetch.Options{CAFile: a.CAFile(), Retries: -1})
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
	return Options{
		Manifest: m, ManifestDir: dir, Lock: p.Lock(m.Name, digest, c.Version), Catalog: c,
		Client: client, Cache: cache, Signer: signer, BaseURL: "http://repo.internal",
	}, a
}

func snapshots(t *testing.T, opts Options) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(Path(opts.Manifest, opts.ManifestDir), "snapshots"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestSync(t *testing.T) {
	opts, _ := newOptions(t, 1)
	var downloaded []download.Target
	opts.Downloaded = func(t download.Target) { downloaded = append(downloaded, t) }
	res, err := Sync(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(opts.ManifestDir, "repos")
	if res.Dir != filepath.Join(root, "current") || len(res.Targets) != 1 || res.Targets[0].Dir != filepath.Join(res.Dir, "apt", "test-1") || res.Targets[0].Files != 2 {
		t.Errorf("result = %+v", res)
	}
	if len(downloaded) != 1 || downloaded[0].Downloaded != 2 {
		t.Errorf("downloaded = %+v", downloaded)
	}
	if link, err := os.Readlink(res.Dir); err != nil || link != filepath.Join("snapshots", res.Snapshot) {
		t.Errorf("current -> %q, %v", link, err)
	}
	for _, name := range []string{
		"apt/test-1/dists/test/InRelease", "apt/test-1/pool/release/main/curl_8.1_amd64.deb",
		"keys/packtrix-repo.gpg", "clients/test-1.sources", "clients/README",
	} {
		if _, err := os.Stat(filepath.Join(res.Dir, name)); err != nil {
			t.Error(err)
		}
	}
	lock, err := manifest.MarshalLock(opts.Lock)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(res.Dir, "packtrix.lock")); err != nil || !bytes.Equal(data, lock) {
		t.Errorf("the snapshot's lock isn't the lock: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(res.Dir, "clients", "test-1.sources")); err != nil || !strings.Contains(string(data), "URIs: http://repo.internal/apt/test-1/\n") {
		t.Errorf("sources = %q, %v", data, err)
	}
	info, err := os.Stat(filepath.Join(root, "snapshots", res.Snapshot))
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("the snapshot's mode is %v, %v", info.Mode(), err)
	}

	// Each sync makes a snapshot, downloads nothing new, and keeps the
	// current snapshot and keepSnapshots earlier ones.
	downloaded = nil
	for range 2 {
		if res, err = Sync(t.Context(), opts); err != nil {
			t.Fatal(err)
		}
	}
	if downloaded[1].Downloaded != 0 {
		t.Errorf("downloaded = %+v", downloaded)
	}
	names := snapshots(t, opts)
	if len(names) != 2 || names[1] != res.Snapshot {
		t.Errorf("snapshots = %q, want two, the last current (%s)", names, res.Snapshot)
	}
}

// A sync that fails leaves the published directory as it was.
func TestFailedSync(t *testing.T) {
	opts, a := newOptions(t, 3)
	first, err := Sync(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}

	// Upstream moves on, and the metadata of the lock isn't cached.
	a.Publish(apttest.Suite{Dir: "debian/dists/test", Fields: releaseFields, Compressions: []string{".xz"}, Indexes: map[string]string{"main/binary-amd64/Packages": ""}})
	opts.Cache = nil
	if _, err := Sync(t.Context(), opts); err == nil || !strings.Contains(err.Error(), "the repository changed since the lock was planned") {
		t.Fatalf("error = %v", err)
	}
	if link, err := os.Readlink(first.Dir); err != nil || link != filepath.Join("snapshots", first.Snapshot) {
		t.Errorf("current -> %q, %v, want the first snapshot", link, err)
	}
	if names := snapshots(t, opts); len(names) != 1 || names[0] != first.Snapshot {
		t.Errorf("snapshots = %q, want only the first", names)
	}
	if _, err := os.Stat(filepath.Join(first.Dir, "apt", "test-1", "dists", "test", "InRelease")); err != nil {
		t.Errorf("the published repository changed: %v", err)
	}
}

func TestSyncRefuses(t *testing.T) {
	opts, _ := newOptions(t, 3)
	root := Path(opts.Manifest, opts.ManifestDir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	unlock, err := lock(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(t.Context(), opts); err == nil || err.Error() != "another sync is writing "+root {
		t.Errorf("while locked: error = %v", err)
	}
	unlock()

	if err := os.Mkdir(filepath.Join(root, "current"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(t.Context(), opts); err == nil || !strings.HasSuffix(err.Error(), "isn't a link to a snapshot; move it aside") {
		t.Errorf("current is a directory: error = %v", err)
	}
}

func TestPath(t *testing.T) {
	opts, _ := newOptions(t, 3)
	opts.Manifest.Spec.Publish.Path = "/srv/repos"
	if got := Path(opts.Manifest, "/etc/packtrix"); got != "/srv/repos" {
		t.Errorf("Path = %s", got)
	}
	opts.Manifest.Spec.Publish.Path = "out"
	if got := Path(opts.Manifest, "/etc/packtrix"); got != "/etc/packtrix/out" {
		t.Errorf("Path = %s", got)
	}
}
