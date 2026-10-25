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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/internal/download"
	"github.com/packtrix/packtrix/internal/expand"
	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/pgp"
	"github.com/packtrix/packtrix/internal/plan"
	"github.com/packtrix/packtrix/internal/pool"
	repoapt "github.com/packtrix/packtrix/internal/repo/apt"
	"github.com/packtrix/packtrix/internal/repo/apt/apttest"
	"github.com/packtrix/packtrix/pkg/apis/packtrix"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
)

// env is everything sync has by the time it builds: an upstream archive,
// a lock planned from it, and the locked packages in the pool.
type env struct {
	archive  *apttest.Archive
	client   *fetch.Client
	cache    *pool.Pool
	packages *pool.Pool
	expanded *expand.Result
	lock     *packtrix.Lock
	signer   *pgp.Signer
	keyring  *pgp.Keyring
}

// deb publishes a package file in the archive and returns its paragraph.
func deb(a *apttest.Archive, name, version, arch, fields string) string {
	filename := fmt.Sprintf("pool/main/%s_%s_%s.deb", name, version, arch)
	content := []byte("the " + filename)
	a.Write("debian/"+filename, content)
	sum := sha256.Sum256(content)
	return fmt.Sprintf("Package: %s\nVersion: %s\nArchitecture: %s\n%sFilename: %s\nSize: %d\nSHA256: %s\nDescription: %s\n\n",
		name, version, arch, fields, filename, len(content), hex.EncodeToString(sum[:]), name)
}

func newEnv(t *testing.T) *env {
	t.Helper()
	upstream, _ := apttest.Key(t)
	a := apttest.New(t, upstream, true)
	fields := "Origin: Debian\nSuite: %s\nCodename: %s\nDate: %s\nArchitectures: amd64 arm64\nComponents: main\nAcquire-By-Hash: yes\n"
	for _, s := range []struct{ suite, date string }{{"test", "Sat, 01 Aug 2026 10:00:00 UTC"}, {"test-updates", "Fri, 23 Oct 2026 10:00:00 UTC"}} {
		indexes := map[string]string{}
		for _, arch := range []string{"amd64", "arm64"} {
			if s.suite == "test" {
				indexes["main/binary-"+arch+"/Packages"] = deb(a, "libc6", "1.0", arch, "Priority: required\n") + deb(a, "tzdata", "2024a", "all", "")
			} else {
				indexes["main/binary-"+arch+"/Packages"] = deb(a, "libc6", "1.1", arch, "Priority: required\n") +
					deb(a, "curl", "8.1", arch, "Depends: libcurl4 (= 8.1), tzdata\n") + deb(a, "libcurl4", "8.1", arch, "Depends: libc6 (>= 1.1)\n")
			}
		}
		a.Publish(apttest.Suite{Dir: "debian/dists/" + s.suite, Fields: fmt.Sprintf(fields, s.suite, s.suite, s.date), Indexes: indexes, Compressions: []string{".xz"}, ByHash: true})
	}

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
architectures: [amd64, arm64]
components: [{ name: main, default: true }]
repositories:
  - { name: release, url: ` + a.URL() + `/debian/, suite: test, keys: [test] }
  - { name: updates, url: ` + a.URL() + `/debian/, suite: test-updates, keys: [test] }
baseline: { repository: release, component: main }
keys: { test: ` + upstream.Fingerprint() + ` }
`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Parse([]byte("apiVersion: packtrix.io/v1alpha1\nkind: OfflineRepository\nmetadata:\n  name: test\nspec:\n  targets:\n    - distro: test-1\n      architectures: [amd64, arm64]\n  packages: [curl]\n  resolve:\n    baseline: none\n"))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{archive: a}
	if e.client, err = fetch.New(fetch.Options{CAFile: a.CAFile(), Retries: -1}); err != nil {
		t.Fatal(err)
	}
	if e.cache, err = pool.Open(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if e.packages, err = pool.Open(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if e.expanded, err = expand.Expand(m, c, ""); err != nil {
		t.Fatal(err)
	}
	p, err := plan.Run(t.Context(), m, c, e.client, plan.Options{Cache: e.cache})
	if err != nil {
		t.Fatal(err)
	}
	e.lock = p.Lock("test", "sha256:"+strings.Repeat("0", 64), "test")
	if _, err := download.Packages(t.Context(), e.client, e.packages, e.lock, download.Options{}); err != nil {
		t.Fatal(err)
	}
	e.signer, e.keyring = apttest.Key(t)
	return e
}

func (e *env) build(t *testing.T, cache *pool.Pool) (string, []Target, error) {
	t.Helper()
	dir := t.TempDir()
	targets, err := APT(t.Context(), e.lock, e.expanded, dir, Options{
		Reader: &repoapt.Reader{Client: e.client, Cache: cache}, Packages: e.packages, Signer: e.signer,
	})
	return dir, targets, err
}

func TestAPT(t *testing.T) {
	e := newEnv(t)
	dir, targets, err := e.build(t, e.cache)
	if err != nil {
		t.Fatal(err)
	}
	// tzdata, of architecture all, is one file for both architectures.
	want := Target{Name: "test-1", Dir: filepath.Join(dir, "apt", "test-1"), Codename: "test", Architectures: []string{"amd64", "arm64"}, Files: 7}
	if len(targets) != 1 || targets[0].Bytes == 0 {
		t.Fatalf("targets = %+v", targets)
	}
	targets[0].Bytes = 0
	if !slices.Equal(targets[0].Architectures, want.Architectures) || targets[0].Name != want.Name || targets[0].Dir != want.Dir ||
		targets[0].Codename != want.Codename || targets[0].Files != want.Files {
		t.Errorf("target = %+v, want %+v", targets[0], want)
	}

	// A machine reads the published repository with the publisher's key
	// only, and gets the locked packages, whose files match the lock.
	server := httptest.NewServer(http.FileServer(http.Dir(want.Dir)))
	defer server.Close()
	plain, err := fetch.New(fetch.Options{Retries: -1})
	if err != nil {
		t.Fatal(err)
	}
	for _, lt := range e.lock.Spec.Targets {
		src := repoapt.Source{Name: "packtrix", URL: server.URL + "/", Suite: "test", Components: []string{"main"}, Keyring: e.keyring}
		r, err := (&repoapt.Reader{Client: plain}).Read(t.Context(), src, lt.Architecture)
		if err != nil {
			t.Fatal(err)
		}
		if r.Release.Origin != Origin || r.Release.Label != "test-1" || r.Release.Date.Format("2006-01-02") != "2026-10-23" {
			t.Errorf("Release = %+v", r.Release)
		}
		var got, wantNames []string
		for _, p := range r.Packages() {
			got = append(got, p.Name+"="+p.Version.String())
			data, err := plain.Get(t.Context(), server.URL+"/"+p.Filename, nil)
			if sum := sha256.Sum256(data); err != nil || hex.EncodeToString(sum[:]) != p.SHA256 || !strings.HasPrefix(p.Filename, "pool/") {
				t.Errorf("%s: %s doesn't match: %v", p.Name, p.Filename, err)
			}
		}
		for _, p := range lt.Packages {
			wantNames = append(wantNames, p.Name+"="+p.Version)
		}
		slices.Sort(got)
		slices.Sort(wantNames)
		if !slices.Equal(got, wantNames) {
			t.Errorf("%s: published %q, want the locked %q", lt.Architecture, got, wantNames)
		}
	}

	// Without the cache, the upstream metadata is read again: it hasn't
	// changed, so the result is the same, apart from the signatures.
	again, _, err := e.build(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	compare(t, dir, again)
}

// compare checks that two builds wrote the same files, apart from the
// signatures.
func compare(t *testing.T, a, b string) {
	t.Helper()
	read := func(root string) map[string][]byte {
		files := map[string][]byte{}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Name() == "InRelease" || d.Name() == "Release.gpg" {
				return err
			}
			data, err := os.ReadFile(path)
			rel, _ := filepath.Rel(root, path)
			files[rel] = data
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	fa, fb := read(a), read(b)
	if len(fa) != len(fb) {
		t.Errorf("%d and %d files", len(fa), len(fb))
	}
	for name, data := range fa {
		if !bytes.Equal(data, fb[name]) {
			t.Errorf("%s differs", name)
		}
	}
}

func TestAPTErrors(t *testing.T) {
	e := newEnv(t)

	empty, err := pool.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	full := e.packages
	e.packages = empty
	if _, _, err := e.build(t, e.cache); err == nil || !strings.HasSuffix(err.Error(), "isn't in the package pool; download it first") {
		t.Errorf("empty pool: error = %v", err)
	}
	e.packages = full

	other := e.lock.DeepCopy()
	other.Spec.Targets[0].Name = "other"
	other.Spec.Targets[1].Name = "other"
	e.lock, other = other, e.lock
	if _, _, err := e.build(t, e.cache); err == nil || err.Error() != "target other: the manifest has no target other; run packtrix plan" {
		t.Errorf("unknown target: error = %v", err)
	}
	e.lock = other

	// Upstream moved on, and the metadata of the lock isn't cached.
	e.archive.Publish(apttest.Suite{
		Dir: "debian/dists/test-updates", Compressions: []string{".xz"},
		Fields:  "Origin: Debian\nSuite: test-updates\nCodename: test-updates\nArchitectures: amd64 arm64\nComponents: main\n",
		Indexes: map[string]string{"main/binary-amd64/Packages": "", "main/binary-arm64/Packages": ""},
	})
	if _, _, err := e.build(t, nil); err == nil || !strings.Contains(err.Error(), "the repository changed since the lock was planned") {
		t.Errorf("changed upstream: error = %v", err)
	}
	// With the cache, the build still works.
	if _, _, err := e.build(t, e.cache); err != nil {
		t.Errorf("with the cache: %v", err)
	}
}
