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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/pool"
	"github.com/packtrix/packtrix/pkg/apis/packtrix"
)

// archive serves package files and counts the requests for each.
type archive struct {
	server *httptest.Server

	mu       sync.Mutex
	files    map[string]string
	requests map[string]int
	auth     bool
}

func newArchive(t *testing.T, files map[string]string) *archive {
	t.Helper()
	a := &archive{files: files, requests: map[string]int{}}
	a.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.requests[r.URL.Path]++
		if user, password, _ := r.BasicAuth(); a.auth && (user != "packtrix" || password != "s3cret") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		content, ok := a.files[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(content))
	}))
	t.Cleanup(a.server.Close)
	return a
}

func (a *archive) total() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, c := range a.requests {
		n += c
	}
	return n
}

func sum(content string) string {
	s := sha256.Sum256([]byte(content))
	return hex.EncodeToString(s[:])
}

// lockPackage is a package whose file the archive serves at path with this
// content.
func lockPackage(a *archive, name, path, content string) packtrix.LockPackage {
	return packtrix.LockPackage{
		Name: name, Version: "1.0", Architecture: "amd64", Repository: "release",
		URL: a.server.URL + path, SHA256: sum(content), Size: int64(len(content)),
	}
}

func setup(t *testing.T) (*archive, *packtrix.Lock, *pool.Pool, *fetch.Client) {
	t.Helper()
	files := map[string]string{
		"/pool/curl.deb":     "curl content",
		"/pool/libcurl4.deb": "libcurl4 content",
		"/pool/tzdata.deb":   "tzdata content, architecture all",
	}
	a := newArchive(t, files)
	curl := lockPackage(a, "curl", "/pool/curl.deb", files["/pool/curl.deb"])
	lib := lockPackage(a, "libcurl4", "/pool/libcurl4.deb", files["/pool/libcurl4.deb"])
	tz := lockPackage(a, "tzdata", "/pool/tzdata.deb", files["/pool/tzdata.deb"])
	l := &packtrix.Lock{Spec: packtrix.LockSpec{Targets: []packtrix.LockTarget{
		{Name: "ubuntu-24.04", Architecture: "amd64", Packages: []packtrix.LockPackage{curl, lib, tz}},
		// The same files for another target.
		{Name: "ubuntu-24.04-gpu", Architecture: "amd64", Packages: []packtrix.LockPackage{curl, tz}},
	}}}
	p, err := pool.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := fetch.New(fetch.Options{Retries: -1})
	if err != nil {
		t.Fatal(err)
	}
	return a, l, p, c
}

func TestPackages(t *testing.T) {
	a, l, p, c := setup(t)
	var done []Target
	got, err := Packages(t.Context(), c, p, l, Options{Done: func(t Target) { done = append(done, t) }})
	if err != nil {
		t.Fatal(err)
	}
	want := []Target{
		{Name: "ubuntu-24.04", Architecture: "amd64", Packages: 3, Downloaded: 3, Bytes: 12 + 16 + 32},
		// Its files are already in the pool.
		{Name: "ubuntu-24.04-gpu", Architecture: "amd64", Packages: 2},
	}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(done, want) {
		t.Errorf("Packages = %+v, Done got %+v, want %+v", got, done, want)
	}
	for _, pkg := range l.Spec.Targets[0].Packages {
		if data, ok, err := p.Read(pkg.SHA256); !ok || err != nil || sum(string(data)) != pkg.SHA256 {
			t.Errorf("%s isn't in the pool", pkg.Name)
		}
	}
	if n := a.total(); n != 3 {
		t.Errorf("%d requests, want one per file", n)
	}

	// A second run downloads nothing.
	got, err = Packages(t.Context(), c, p, l, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Downloaded != 0 || a.total() != 3 {
		t.Errorf("second run: %+v, %d requests", got, a.total())
	}
}

func TestTamperedFile(t *testing.T) {
	a, l, p, c := setup(t)
	a.files["/pool/libcurl4.deb"] = "libcurl4 CONTENT"
	_, err := Packages(t.Context(), c, p, l, Options{Workers: 1})
	lib := l.Spec.Targets[0].Packages[1]
	want := "target ubuntu-24.04 (amd64): package libcurl4 1.0 (" + lib.URL + "): doesn't match the lock: got 16 bytes with SHA-256 " +
		sum("libcurl4 CONTENT") + ", want 16 bytes with SHA-256 " + lib.SHA256
	if err == nil || err.Error() != want {
		t.Errorf("error = %v\nwant %s", err, want)
	}
	if p.Has(lib.SHA256) {
		t.Error("the tampered file is in the pool")
	}
	if _, err := os.Stat(p.Partial(lib.SHA256)); !os.IsNotExist(err) {
		t.Errorf("the tampered download was kept: %v", err)
	}
	// Downloading stopped at the failure.
	if a.requests["/pool/tzdata.deb"] != 0 {
		t.Errorf("requests = %v, want none for tzdata", a.requests)
	}
}

func TestFinishedDownload(t *testing.T) {
	a, l, p, c := setup(t)
	curl := l.Spec.Targets[0].Packages[0]
	// An earlier run downloaded curl but stopped before committing it.
	if err := os.WriteFile(p.Partial(curl.SHA256), []byte("curl content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Packages(t.Context(), c, p, l, Options{}); err != nil {
		t.Fatal(err)
	}
	if n := a.requests["/pool/curl.deb"]; n != 0 || !p.Has(curl.SHA256) {
		t.Errorf("curl: %d requests, in the pool %t", n, p.Has(curl.SHA256))
	}
}

func TestAuth(t *testing.T) {
	a, l, p, c := setup(t)
	a.auth = true
	var asked []string
	var mu sync.Mutex
	auth := func(target, repository string) (*fetch.Auth, error) {
		mu.Lock()
		asked = append(asked, target+"/"+repository)
		mu.Unlock()
		dir := t.TempDir()
		if err := os.WriteFile(dir+"/password", []byte("s3cret"), 0o600); err != nil {
			return nil, err
		}
		return fetch.ResolveAuth(&packtrix.Auth{Username: "packtrix", Password: "file:password"}, dir)
	}
	if _, err := Packages(t.Context(), c, p, l, Options{Auth: auth}); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 3 || !strings.HasPrefix(asked[0], "ubuntu-24.04/release") {
		t.Errorf("Auth asked for %q", asked)
	}

	_, l, p, c = setup(t)
	_, err := Packages(t.Context(), c, p, l, Options{Auth: func(string, string) (*fetch.Auth, error) {
		return nil, errors.New("no credentials")
	}})
	if err == nil || !strings.HasSuffix(err.Error(), "): no credentials") {
		t.Errorf("error = %v", err)
	}
}

func TestErrors(t *testing.T) {
	a, l, p, c := setup(t)
	delete(a.files, "/pool/curl.deb")
	if _, err := Packages(t.Context(), c, p, l, Options{}); err == nil || !strings.HasSuffix(err.Error(), "/pool/curl.deb: 404 Not Found") {
		t.Errorf("error = %v", err)
	}

	_, l, p, c = setup(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Packages(ctx, c, p, l, Options{}); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: error = %v", err)
	}
}
