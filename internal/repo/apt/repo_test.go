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
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
	"github.com/ulikunitz/xz"

	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/pgp"
)

// archiveKey returns a signer, like an archive's, and a keyring that pins
// it.
func archiveKey(t *testing.T) (*pgp.Signer, *pgp.Keyring) {
	t.Helper()
	e, err := openpgp.NewEntity("Test Archive Automatic Signing Key", "", "archive@example.org", &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PrivateKeyType, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SerializePrivateWithoutSigning(w, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := pgp.LoadSigner(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	public, err := s.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	k, err := pgp.NewKeyring(public, s.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	return s, k
}

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

// archive is an APT repository on disk, served over HTTP.
type archive struct {
	t      *testing.T
	root   string
	signer *pgp.Signer
	server *httptest.Server

	mu       sync.Mutex
	requests []string
}

func newArchive(t *testing.T, signer *pgp.Signer) *archive {
	t.Helper()
	a := &archive{t: t, root: t.TempDir(), signer: signer}
	files := http.FileServer(http.Dir(a.root))
	a.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.requests = append(a.requests, r.URL.Path)
		a.mu.Unlock()
		files.ServeHTTP(w, r)
	}))
	t.Cleanup(a.server.Close)
	return a
}

// suite describes a Release file and its indexes.
type suite struct {
	// dir is the Release's directory, such as "dists/noble", or "" for a
	// flat repository.
	dir    string
	fields string
	// indexes are Packages contents, by path without extension.
	indexes      map[string]string
	compressions []string
	byHash       bool
	detached     bool
}

func (a *archive) write(name string, data []byte) {
	a.t.Helper()
	file := filepath.Join(a.root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		a.t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		a.t.Fatal(err)
	}
}

func compress(t *testing.T, text, ext string) []byte {
	t.Helper()
	var buf bytes.Buffer
	var w interface {
		Write([]byte) (int, error)
		Close() error
	}
	switch ext {
	case ".xz":
		xw, err := xz.NewWriter(&buf)
		if err != nil {
			t.Fatal(err)
		}
		w = xw
	case ".gz":
		w = gzip.NewWriter(&buf)
	default:
		return []byte(text)
	}
	if _, err := w.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// publish writes a suite's indexes and its signed Release.
func (a *archive) publish(s suite) {
	a.t.Helper()
	var hashes strings.Builder
	var indexes []string
	for index := range s.indexes {
		indexes = append(indexes, index)
	}
	slices.Sort(indexes)
	for _, index := range indexes {
		for _, ext := range s.compressions {
			data := compress(a.t, s.indexes[index], ext)
			sum := sha256.Sum256(data)
			hash := hex.EncodeToString(sum[:])
			a.write(path.Join(s.dir, index+ext), data)
			if s.byHash {
				a.write(path.Join(s.dir, path.Dir(index), "by-hash/SHA256", hash), data)
			}
			fmt.Fprintf(&hashes, " %s %d %s\n", hash, len(data), index+ext)
		}
	}
	release := []byte(s.fields + "SHA256:\n" + hashes.String())
	if s.detached {
		sig, err := a.signer.DetachSign(release)
		if err != nil {
			a.t.Fatal(err)
		}
		a.write(path.Join(s.dir, "Release"), release)
		a.write(path.Join(s.dir, "Release.gpg"), sig)
		return
	}
	inRelease, err := a.signer.Clearsign(release)
	if err != nil {
		a.t.Fatal(err)
	}
	a.write(path.Join(s.dir, "InRelease"), inRelease)
}

func (a *archive) read(keyring *pgp.Keyring, suiteName string, components ...string) (*Repository, error) {
	a.t.Helper()
	c, err := fetch.New(fetch.Options{Retries: -1})
	if err != nil {
		a.t.Fatal(err)
	}
	a.mu.Lock()
	a.requests = nil
	a.mu.Unlock()
	src := Source{Name: "test", URL: a.server.URL + "/ubuntu/", Suite: suiteName, Components: components, Keyring: keyring}
	return Read(a.t.Context(), c, src, "amd64")
}

func (a *archive) requested() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.requests)
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
	signer, keyring := archiveKey(t)
	a := newArchive(t, signer)
	a.publish(suite{
		dir:    "ubuntu/dists/noble",
		fields: nobleFields,
		indexes: map[string]string{
			"main/binary-amd64/Packages":     packages("curl", "libcurl4"),
			"universe/binary-amd64/Packages": packages("jq"),
		},
		compressions: []string{".xz", ".gz", ""},
		byHash:       true,
	})

	r, err := a.read(keyring, "noble", "main", "universe")
	if err != nil {
		t.Fatal(err)
	}
	if got := names(r); !slices.Equal(got, []string{"curl", "libcurl4", "jq"}) {
		t.Errorf("packages = %q", got)
	}
	if r.Signer != signer.Fingerprint() || r.Release.Origin != "Ubuntu" || r.Release.Version != "24.04" || r.Release.Distribution() != "Ubuntu noble" {
		t.Errorf("repository = %+v, release = %+v", r, r.Release)
	}
	curl := r.Indexes[0].Packages[0]
	if r.Indexes[0].Component != "main" || r.Indexes[0].Architecture != "amd64" || curl.Filename != "pool/main/curl_1.0_amd64.deb" || curl.Size != 1000 {
		t.Errorf("index = %+v, curl = %+v", r.Indexes[0], curl)
	}
	// Only the xz index, by hash.
	for _, p := range a.requested() {
		if p != "/ubuntu/dists/noble/InRelease" && !strings.Contains(p, "/by-hash/SHA256/") {
			t.Errorf("requested %s, want only the InRelease and indexes by hash", p)
		}
	}
	if n := len(a.requested()); n != 3 {
		t.Errorf("%d requests, want 3: %q", n, a.requested())
	}
}

func TestReadFallbacks(t *testing.T) {
	signer, keyring := archiveKey(t)
	for _, tt := range []struct {
		name         string
		suite        suite
		suiteName    string
		components   []string
		wantRequests []string
	}{
		{"Release and Release.gpg without InRelease", suite{
			dir: "ubuntu/dists/noble", fields: nobleFields, detached: true, byHash: true,
			indexes: map[string]string{"main/binary-amd64/Packages": packages("curl")}, compressions: []string{".gz"},
		}, "noble", []string{"main"}, []string{"InRelease", "Release", "Release.gpg", "main/binary-amd64/by-hash/SHA256/"}},
		{"by-hash missing", suite{
			dir: "ubuntu/dists/noble", fields: nobleFields,
			indexes: map[string]string{"main/binary-amd64/Packages": packages("curl")}, compressions: []string{".gz"},
		}, "noble", []string{"main"}, []string{"InRelease", "main/binary-amd64/by-hash/SHA256/", "main/binary-amd64/Packages.gz"}},
		{"uncompressed only", suite{
			dir: "ubuntu/dists/noble", fields: strings.Replace(nobleFields, "Acquire-By-Hash: yes\n", "", 1),
			indexes: map[string]string{"main/binary-amd64/Packages": packages("curl")}, compressions: []string{""},
		}, "noble", []string{"main"}, []string{"InRelease", "main/binary-amd64/Packages"}},
		{"flat repository", suite{
			dir: "ubuntu", fields: "Origin: obs://build.opensuse.org/isv:kubernetes\nLabel: isv:kubernetes\n",
			indexes: map[string]string{"Packages": packages("curl")}, compressions: []string{".gz"},
		}, "/", nil, []string{"InRelease", "Packages.gz"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := newArchive(t, signer)
			a.publish(tt.suite)
			r, err := a.read(keyring, tt.suiteName, tt.components...)
			if err != nil {
				t.Fatal(err)
			}
			if got := names(r); !slices.Equal(got, []string{"curl"}) {
				t.Errorf("packages = %q", got)
			}
			got := a.requested()
			if len(got) != len(tt.wantRequests) {
				t.Fatalf("requests = %q, want %q", got, tt.wantRequests)
			}
			for i, want := range tt.wantRequests {
				if !strings.HasPrefix(got[i], "/"+tt.suite.dir+"/"+want) {
					t.Errorf("request %d = %s, want %s", i, got[i], want)
				}
			}
		})
	}
}

func TestArchitectureAll(t *testing.T) {
	signer, keyring := archiveKey(t)
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
			a := newArchive(t, signer)
			a.publish(suite{dir: "ubuntu/dists/noble", fields: tt.fields, indexes: indexes, compressions: []string{".xz"}, byHash: true})
			r, err := a.read(keyring, "noble", "main")
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
	signer, keyring := archiveKey(t)
	_, otherKeyring := archiveKey(t)
	good := suite{
		dir: "ubuntu/dists/noble", fields: nobleFields, byHash: true, compressions: []string{".xz"},
		indexes: map[string]string{"main/binary-amd64/Packages": packages("curl")},
	}
	for _, tt := range []struct {
		name       string
		suite      suite
		tamper     func(a *archive)
		keyring    *pgp.Keyring
		suiteName  string
		components []string
		want       string
	}{
		{name: "unpinned key", suite: good, keyring: otherKeyring, want: "InRelease: signed by key " + signer.Fingerprint() + ", which isn't pinned"},
		{name: "tampered index", suite: good, tamper: func(a *archive) {
			for _, name := range []string{"main/binary-amd64/Packages.xz"} {
				data := compress(a.t, packages("curl", "evil"), ".xz")
				a.write("ubuntu/dists/noble/"+name, data)
				entries, _ := os.ReadDir(filepath.Join(a.root, "ubuntu/dists/noble/main/binary-amd64/by-hash/SHA256"))
				for _, e := range entries {
					a.write("ubuntu/dists/noble/main/binary-amd64/by-hash/SHA256/"+e.Name(), data)
				}
			}
		}, want: "main/binary-amd64/Packages.xz doesn't match the signed Release file"},
		{name: "tampered InRelease", suite: good, tamper: func(a *archive) {
			file := filepath.Join(a.root, "ubuntu/dists/noble/InRelease")
			data, _ := os.ReadFile(file)
			a.write("ubuntu/dists/noble/InRelease", bytes.Replace(data, []byte("Origin: Ubuntu"), []byte("Origin: Ubunty"), 1))
		}, want: "InRelease: bad signature"},
		// A server, or a mirror on the way, answers for noble-updates with
		// the Release of noble.
		{name: "other suite", suite: suite{
			dir: "ubuntu/dists/noble-updates", fields: nobleFields, compressions: []string{".xz"}, indexes: good.indexes,
		}, suiteName: "noble-updates", want: `the Release file is for suite "noble" (noble), not "noble-updates"`},
		{name: "expired", suite: suite{
			dir: "ubuntu/dists/noble", fields: regexp.MustCompile(`Valid-Until: .*`).ReplaceAllString(nobleFields, "Valid-Until: Sat, 01 Jan 2000 00:00:00 UTC"),
			compressions: []string{".xz"}, indexes: good.indexes,
		}, want: "the Release file expired on Sat, 01 Jan 2000 00:00:00 UTC"},
		{name: "missing component", suite: good, components: []string{"main", "restricted"}, want: "the Release file lists no restricted/binary-amd64/Packages index"},
		{name: "no InRelease, no Release", suite: suite{dir: "ubuntu/dists/other"}, want: "/ubuntu/dists/noble/Release: 404 Not Found"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := newArchive(t, signer)
			if tt.suite.indexes != nil {
				a.publish(tt.suite)
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
			_, err := a.read(k, cmp.Or(tt.suiteName, "noble"), components...)
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.HasPrefix(err.Error(), "repository test ("+a.server.URL+"/ubuntu/ ") {
				t.Errorf("error = %v, want it to contain %q", err, tt.want)
			}
			// A bad InRelease never falls back to the unsigned Release.
			if slices.Contains(a.requested(), "/ubuntu/dists/noble/Release") && tt.suite.indexes != nil {
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
