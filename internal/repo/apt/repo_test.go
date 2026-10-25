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
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/pgp"
	"github.com/packtrix/packtrix/internal/pool"
	"github.com/packtrix/packtrix/internal/repo/apt/apttest"
)

func packages(names ...string) string {
	var b strings.Builder
	for _, name := range names {
		arch := "amd64"
		if strings.HasSuffix(name, "-data") {
			arch = "all"
		}
		fmt.Fprintf(&b, "Package: %s\nVersion: 1.0\nArchitecture: %s\nFilename: pool/main/%s_1.0_%s.deb\nSize: 1000\nSHA256: %s\n\n",
			name, arch, name, arch, strings.Repeat("ab", 32))
	}
	return b.String()
}

// read reads the archive's repository at /ubuntu/ for amd64.
func read(t *testing.T, a *apttest.Archive, keyring *pgp.Keyring, suiteName string, components ...string) (*Repository, error) {
	t.Helper()
	c, err := fetch.New(fetch.Options{Retries: -1})
	if err != nil {
		t.Fatal(err)
	}
	a.ResetRequests()
	src := Source{Name: "test", URL: a.URL() + "/ubuntu/", Suite: suiteName, Components: components, Keyring: keyring}
	return (&Reader{Client: c}).Read(t.Context(), src, "amd64")
}

func validUntil(d time.Duration) string {
	return time.Now().Add(d).UTC().Format(time.RFC1123)
}

var nobleFields = `Origin: Ubuntu
Label: Ubuntu
Suite: noble
Codename: noble
Version: 24.04
Valid-Until: ` + validUntil(7*24*time.Hour) + `
Architectures: amd64 arm64
Components: main universe
Acquire-By-Hash: yes
`

func names(r *Repository) []string {
	var out []string
	for _, p := range r.Packages() {
		out = append(out, p.Name)
	}
	return out
}

func TestRead(t *testing.T) {
	signer, keyring := apttest.Key(t)
	a := apttest.New(t, signer, false)
	a.Publish(apttest.Suite{
		Dir:    "ubuntu/dists/noble",
		Fields: nobleFields,
		Indexes: map[string]string{
			"main/binary-amd64/Packages":     packages("curl", "libcurl4"),
			"universe/binary-amd64/Packages": packages("jq"),
		},
		Compressions: []string{".xz", ".gz", ""},
		ByHash:       true,
	})

	r, err := read(t, a, keyring, "noble", "main", "universe")
	if err != nil {
		t.Fatal(err)
	}
	if got := names(r); !slices.Equal(got, []string{"curl", "libcurl4", "jq"}) {
		t.Errorf("packages = %q", got)
	}
	if r.Signer != signer.Fingerprint() || r.Release.Origin != "Ubuntu" || r.Release.Version != "24.04" || r.Release.Distribution() != "Ubuntu noble" || !strings.HasPrefix(r.Digest, "sha256:") || len(r.Digest) != 71 {
		t.Errorf("repository = %+v, release = %+v", r, r.Release)
	}
	curl := r.Indexes[0].Packages[0]
	if r.Indexes[0].Component != "main" || r.Indexes[0].Architecture != "amd64" || curl.Filename != "pool/main/curl_1.0_amd64.deb" || curl.Size != 1000 {
		t.Errorf("index = %+v, curl = %+v", r.Indexes[0], curl)
	}
	// Only the xz index, by hash.
	for _, p := range a.Requests() {
		if p != "/ubuntu/dists/noble/InRelease" && !strings.Contains(p, "/by-hash/SHA256/") {
			t.Errorf("requested %s, want only the InRelease and indexes by hash", p)
		}
	}
	if n := len(a.Requests()); n != 3 {
		t.Errorf("%d requests, want 3: %q", n, a.Requests())
	}
}

func TestReadFallbacks(t *testing.T) {
	signer, keyring := apttest.Key(t)
	for _, tt := range []struct {
		name         string
		suite        apttest.Suite
		suiteName    string
		components   []string
		wantRequests []string
	}{
		{"Release and Release.gpg without InRelease", apttest.Suite{
			Dir: "ubuntu/dists/noble", Fields: nobleFields, Detached: true, ByHash: true,
			Indexes: map[string]string{"main/binary-amd64/Packages": packages("curl")}, Compressions: []string{".gz"},
		}, "noble", []string{"main"}, []string{"InRelease", "Release", "Release.gpg", "main/binary-amd64/by-hash/SHA256/"}},
		{"by-hash missing", apttest.Suite{
			Dir: "ubuntu/dists/noble", Fields: nobleFields,
			Indexes: map[string]string{"main/binary-amd64/Packages": packages("curl")}, Compressions: []string{".gz"},
		}, "noble", []string{"main"}, []string{"InRelease", "main/binary-amd64/by-hash/SHA256/", "main/binary-amd64/Packages.gz"}},
		{"uncompressed only", apttest.Suite{
			Dir: "ubuntu/dists/noble", Fields: strings.Replace(nobleFields, "Acquire-By-Hash: yes\n", "", 1),
			Indexes: map[string]string{"main/binary-amd64/Packages": packages("curl")}, Compressions: []string{""},
		}, "noble", []string{"main"}, []string{"InRelease", "main/binary-amd64/Packages"}},
		{"flat repository", apttest.Suite{
			Dir: "ubuntu", Fields: "Origin: obs://build.opensuse.org/isv:kubernetes\nLabel: isv:kubernetes\n",
			Indexes: map[string]string{"Packages": packages("curl")}, Compressions: []string{".gz"},
		}, "/", nil, []string{"InRelease", "Packages.gz"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := apttest.New(t, signer, false)
			a.Publish(tt.suite)
			r, err := read(t, a, keyring, tt.suiteName, tt.components...)
			if err != nil {
				t.Fatal(err)
			}
			if got := names(r); !slices.Equal(got, []string{"curl"}) {
				t.Errorf("packages = %q", got)
			}
			got := a.Requests()
			if len(got) != len(tt.wantRequests) {
				t.Fatalf("requests = %q, want %q", got, tt.wantRequests)
			}
			for i, want := range tt.wantRequests {
				if !strings.HasPrefix(got[i], "/"+tt.suite.Dir+"/"+want) {
					t.Errorf("request %d = %s, want %s", i, got[i], want)
				}
			}
		})
	}
}

func TestArchitectureAll(t *testing.T) {
	signer, keyring := apttest.Key(t)
	for _, tt := range []struct {
		name   string
		fields string
		all    bool
		want   []string
	}{
		{"listed", nobleFields, true, []string{"curl", "curl-data"}},
		{"already in the architecture's index", nobleFields + "No-Support-for-Architecture-all: Packages\n", true, []string{"curl"}},
		{"not listed", nobleFields, false, []string{"curl"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			indexes := map[string]string{"main/binary-amd64/Packages": packages("curl")}
			if tt.all {
				indexes["main/binary-all/Packages"] = packages("curl-data")
			}
			a := apttest.New(t, signer, false)
			a.Publish(apttest.Suite{Dir: "ubuntu/dists/noble", Fields: tt.fields, Indexes: indexes, Compressions: []string{".xz"}, ByHash: true})
			r, err := read(t, a, keyring, "noble", "main")
			if err != nil {
				t.Fatal(err)
			}
			if got := names(r); !slices.Equal(got, tt.want) {
				t.Errorf("packages = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadRejects(t *testing.T) {
	signer, keyring := apttest.Key(t)
	_, otherKeyring := apttest.Key(t)
	good := apttest.Suite{
		Dir: "ubuntu/dists/noble", Fields: nobleFields, ByHash: true, Compressions: []string{".xz"},
		Indexes: map[string]string{"main/binary-amd64/Packages": packages("curl")},
	}
	for _, tt := range []struct {
		name       string
		suite      apttest.Suite
		tamper     func(a *apttest.Archive)
		keyring    *pgp.Keyring
		suiteName  string
		components []string
		want       string
	}{
		{name: "unpinned key", suite: good, keyring: otherKeyring, want: "InRelease: signed by key " + signer.Fingerprint() + ", which isn't pinned"},
		{name: "tampered index", suite: good, tamper: func(a *apttest.Archive) {
			sum := sha256.Sum256(apttest.Compress(t, packages("curl"), ".xz"))
			evil := apttest.Compress(t, packages("curl", "evil"), ".xz")
			a.Write("ubuntu/dists/noble/main/binary-amd64/Packages.xz", evil)
			a.Write("ubuntu/dists/noble/main/binary-amd64/by-hash/SHA256/"+hex.EncodeToString(sum[:]), evil)
		}, want: "main/binary-amd64/Packages.xz doesn't match the signed Release file"},
		{name: "tampered InRelease", suite: good, tamper: func(a *apttest.Archive) {
			data := a.Read("ubuntu/dists/noble/InRelease")
			a.Write("ubuntu/dists/noble/InRelease", bytes.Replace(data, []byte("Origin: Ubuntu"), []byte("Origin: Ubunty"), 1))
		}, want: "InRelease: bad signature"},
		// A server, or a mirror on the way, answers for noble-updates with
		// the Release of noble.
		{name: "other suite", suite: apttest.Suite{
			Dir: "ubuntu/dists/noble-updates", Fields: nobleFields, Compressions: []string{".xz"}, Indexes: good.Indexes,
		}, suiteName: "noble-updates", want: `the Release file is for suite "noble" (noble), not "noble-updates"`},
		{name: "expired", suite: apttest.Suite{
			Dir: "ubuntu/dists/noble", Fields: regexp.MustCompile(`Valid-Until: .*`).ReplaceAllString(nobleFields, "Valid-Until: Sat, 01 Jan 2000 00:00:00 UTC"),
			Compressions: []string{".xz"}, Indexes: good.Indexes,
		}, want: "the Release file expired on Sat, 01 Jan 2000 00:00:00 UTC"},
		{name: "missing component", suite: good, components: []string{"main", "restricted"}, want: "the Release file lists no restricted/binary-amd64/Packages index"},
		{name: "no InRelease, no Release", suite: apttest.Suite{Dir: "ubuntu/dists/other"}, want: "/ubuntu/dists/noble/Release: 404 Not Found"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := apttest.New(t, signer, false)
			if tt.suite.Indexes != nil {
				a.Publish(tt.suite)
			}
			if tt.tamper != nil {
				tt.tamper(a)
			}
			k, components := tt.keyring, tt.components
			if k == nil {
				k = keyring
			}
			if components == nil {
				components = []string{"main"}
			}
			_, err := read(t, a, k, cmp.Or(tt.suiteName, "noble"), components...)
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.HasPrefix(err.Error(), "repository test ("+a.URL()+"/ubuntu/ ") {
				t.Errorf("error = %v, want it to contain %q", err, tt.want)
			}
			// A bad InRelease never falls back to the unsigned Release.
			if slices.Contains(a.Requests(), "/ubuntu/dists/noble/Release") && tt.suite.Indexes != nil {
				t.Errorf("requested the Release after a bad InRelease")
			}
		})
	}
}

func TestParseReleaseErrors(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, tt := range []struct{ text, want string }{
		{"Origin: Ubuntu\n", "the Release file has no SHA256 hashes"},
		{"Origin: Ubuntu\nSHA256:\n abc 12 main/binary-amd64/Packages\n", `the Release file has an invalid SHA256 line "abc 12 main/binary-amd64/Packages"`},
		{"Origin: Ubuntu\nSHA256:\n " + hash + " -1 Packages\n", "the Release file has an invalid size in"},
		{"Origin: Ubuntu\nValid-Until: tomorrow\nSHA256:\n " + hash + " 1 Packages\n", `the Release file's Valid-Until "tomorrow" isn't a date`},
		{"Origin: Ubuntu\n\nOrigin: Debian\n", "the Release file has 2 paragraphs, not 1"},
		{"Origin: Ubuntu\n continued\n not a field\nbroken\n", "reading the Release file: "},
	} {
		if _, err := ParseRelease([]byte(tt.text)); err == nil || !strings.HasPrefix(err.Error(), tt.want) {
			t.Errorf("ParseRelease(%q) = %v, want %q", tt.text, err, tt.want)
		}
	}
}

func TestCheckSameRelease(t *testing.T) {
	repo := func(name, origin, codename string) *Repository {
		return &Repository{Name: name, Release: &Release{Origin: origin, Codename: codename}}
	}
	for _, tt := range []struct {
		repos []*Repository
		want  string
	}{
		{[]*Repository{repo("release", "Ubuntu", "noble"), repo("updates", "Ubuntu", "noble")}, ""},
		{[]*Repository{repo("release", "Debian", "bookworm"), repo("security", "Debian", "bookworm-security")}, ""},
		{nil, ""},
		{[]*Repository{repo("release", "Ubuntu", "noble"), repo("old", "Ubuntu", "jammy")},
			"repositories release and old are for different releases: Ubuntu noble and Ubuntu jammy; a target can't mix releases"},
		{[]*Repository{repo("release", "Ubuntu", "noble"), repo("debian", "Debian", "noble")},
			"repositories release and debian are for different releases: Ubuntu noble and Debian noble; a target can't mix releases"},
	} {
		err := CheckSameRelease(tt.repos)
		if (err == nil) != (tt.want == "") || err != nil && err.Error() != tt.want {
			t.Errorf("CheckSameRelease = %v, want %q", err, tt.want)
		}
	}
}

func TestReadCache(t *testing.T) {
	signer, keyring := apttest.Key(t)
	a := apttest.New(t, signer, false)
	a.Publish(apttest.Suite{
		Dir: "ubuntu/dists/noble", Fields: nobleFields, ByHash: true, Compressions: []string{".xz"},
		Indexes: map[string]string{"main/binary-amd64/Packages": packages("curl")},
	})
	cache, err := pool.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client, err := fetch.New(fetch.Options{Retries: -1})
	if err != nil {
		t.Fatal(err)
	}
	reader := &Reader{Client: client, Cache: cache}
	src := Source{Name: "test", URL: a.URL() + "/ubuntu/", Suite: "noble", Components: []string{"main"}, Keyring: keyring}
	read := func(wantRequests int) {
		t.Helper()
		a.ResetRequests()
		r, err := reader.Read(t.Context(), src, "amd64")
		if err != nil {
			t.Fatal(err)
		}
		if got := names(r); !slices.Equal(got, []string{"curl"}) {
			t.Errorf("packages = %q", got)
		}
		if got := a.Requests(); len(got) != wantRequests {
			t.Errorf("requests = %q, want %d", got, wantRequests)
		}
	}

	read(2)
	// The Release is always fetched; the index comes from the cache.
	read(1)

	// A cached index changed behind Packtrix's back is fetched again.
	sum := sha256.Sum256(apttest.Compress(t, packages("curl"), ".xz"))
	cached := cache.Path(hex.EncodeToString(sum[:]))
	if err := os.Chmod(cached, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cached, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	read(2)
	read(1)
}

func TestParagraphs(t *testing.T) {
	signer, keyring := apttest.Key(t)
	a := apttest.New(t, signer, false)
	publish := func(names ...string) {
		a.Publish(apttest.Suite{
			Dir: "ubuntu/dists/noble", Fields: nobleFields, ByHash: true, Compressions: []string{".xz"},
			Indexes: map[string]string{"main/binary-amd64/Packages": packages(names...)},
		})
	}
	publish("curl", "jq")
	cache, err := pool.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client, err := fetch.New(fetch.Options{Retries: -1})
	if err != nil {
		t.Fatal(err)
	}
	src := Source{Name: "test", URL: a.URL() + "/ubuntu/", Suite: "noble", Components: []string{"main"}, Keyring: keyring}
	cached := &Reader{Client: client, Cache: cache}
	r, err := cached.Read(t.Context(), src, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	digest := r.Digest
	want := map[Key]bool{{Name: "curl", Version: "1.0", Architecture: "amd64"}: true}
	curl := strings.SplitAfter(packages("curl"), "\n\n")[0]
	curl = strings.TrimSuffix(curl, "\n")

	paragraphs := func(rd *Reader) (map[Key]string, error) {
		t.Helper()
		a.ResetRequests()
		found, rel, err := rd.Paragraphs(t.Context(), src, "amd64", digest, want)
		if err == nil && rel.Codename != "noble" {
			t.Errorf("Release = %+v", rel)
		}
		return found, err
	}
	check := func(found map[Key]string, err error, requests int) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 1 || found[Key{"curl", "1.0", "amd64"}] != curl {
			t.Errorf("paragraphs = %q, want %q", found, curl)
		}
		if got := a.Requests(); len(got) != requests {
			t.Errorf("requests = %q, want %d", got, requests)
		}
	}

	// From the cache, the repository isn't asked anything.
	found, err := paragraphs(cached)
	check(found, err, 0)
	// Without a cache, it is read again, and must still be the same.
	found, err = paragraphs(&Reader{Client: client})
	check(found, err, 2)

	// Upstream moves on: the cache still has it as plan saw it.
	publish("curl", "jq", "wget")
	found, err = paragraphs(cached)
	check(found, err, 0)
	_, err = paragraphs(&Reader{Client: client})
	if err == nil || !strings.HasSuffix(err.Error(), "the repository changed since the lock was planned, and the cache doesn't hold it as it was; run packtrix plan") {
		t.Errorf("error = %v", err)
	}
}
