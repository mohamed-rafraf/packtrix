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
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/pgp"
	"github.com/packtrix/packtrix/internal/pool"
	"github.com/packtrix/packtrix/internal/repo/apt/apttest"
)

func paragraph(name, arch, filename string) string {
	return "Package: " + name + "\nVersion: 1.0\nArchitecture: " + arch + "\nDepends: libc6 (>= 2.34)\nFilename: " + filename +
		"\nSize: 10\nSHA256: " + strings.Repeat("ab", 32) + "\nDescription: " + name + "\n with a second line\n"
}

// testSuite writes the files of a suite into a pool and returns the suite.
func testSuite(t *testing.T) (Suite, *pool.Pool) {
	t.Helper()
	p, err := pool.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var published []Published
	for _, f := range []struct{ name, arch, index string }{
		{"curl", "amd64", "amd64"},
		{"curl", "arm64", "arm64"},
		{"tzdata", "all", "amd64"},
		{"tzdata", "all", "arm64"},
	} {
		content := f.name + " for " + f.arch
		s := sha256.Sum256([]byte(content))
		sum := hex.EncodeToString(s[:])
		if err := p.Add(strings.NewReader(content), sum); err != nil {
			t.Fatal(err)
		}
		filename := "pool/updates/main/" + f.name + "_1.0_" + f.arch + ".deb"
		published = append(published, Published{
			Name: f.name, Version: "1.0", Architecture: f.index, SHA256: sum, Filename: filename,
			Paragraph: paragraph(f.name, f.arch, "pool/main/upstream/"+f.name+".deb"),
		})
	}
	return Suite{
		Origin: "Packtrix", Label: "ubuntu-24.04", Codename: "noble", Architectures: []string{"arm64", "amd64"},
		Date: time.Date(2026, 10, 24, 21, 34, 29, 0, time.UTC), Packages: published,
	}, p
}

func TestWrite(t *testing.T) {
	suite, packages := testSuite(t)
	signer, keyring := apttest.Key(t)
	dir := t.TempDir()
	if err := Write(dir, suite, packages, signer); err != nil {
		t.Fatal(err)
	}
	dists := filepath.Join(dir, "dists", "noble")
	rel := verifyRelease(t, dists, keyring, signer.Fingerprint())
	if rel.Origin != "Packtrix" || rel.Label != "ubuntu-24.04" || rel.Suite != "noble" || rel.Codename != "noble" || !rel.AcquireByHash ||
		!rel.ValidUntil.IsZero() || !rel.Date.Equal(suite.Date) || strings.Join(rel.Architectures, " ") != "amd64 arm64" || strings.Join(rel.Components, " ") != "main" {
		t.Errorf("Release = %+v", rel)
	}
	if len(rel.Files) != 6 {
		t.Errorf("the Release lists %d files, want 3 per architecture", len(rel.Files))
	}
	checkHashes(t, dists, rel)

	// Each index holds its packages, with the published Filename and the
	// rest of the upstream paragraph unchanged.
	index, err := os.ReadFile(filepath.Join(dists, "main", "binary-amd64", "Packages"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(paragraph("curl", "amd64", ""), "Filename: \n", "Filename: pool/updates/main/curl_1.0_amd64.deb\n", 1) + "\n" +
		strings.Replace(paragraph("tzdata", "all", ""), "Filename: \n", "Filename: pool/updates/main/tzdata_1.0_all.deb\n", 1) + "\n"
	if string(index) != want {
		t.Errorf("Packages =\n%s\nwant\n%s", index, want)
	}
	for _, p := range suite.Packages {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p.Filename)))
		if err != nil || !strings.HasPrefix(string(data), p.Name+" for ") {
			t.Errorf("%s: %q, %v", p.Filename, data, err)
		}
	}

	// A machine reads the repository with the published key only.
	server := httptest.NewServer(http.FileServer(http.Dir(dir)))
	defer server.Close()
	client, err := fetch.New(fetch.Options{Retries: -1})
	if err != nil {
		t.Fatal(err)
	}
	r, err := (&Reader{Client: client}).Read(t.Context(), Source{Name: "packtrix", URL: server.URL + "/", Suite: "noble", Components: []string{"main"}, Keyring: keyring}, "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if got := names(r); !slices.Equal(got, []string{"curl", "tzdata"}) {
		t.Errorf("arm64 packages = %q", got)
	}
}

// verifyRelease checks that InRelease and Release.gpg sign the Release, and
// returns it.
func verifyRelease(t *testing.T, dists string, keyring *pgp.Keyring, fingerprint string) *Release {
	t.Helper()
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dists, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	release := read("Release")
	text, signer, err := keyring.VerifyClearsigned(read("InRelease"))
	if err != nil || signer != fingerprint {
		t.Fatalf("InRelease: %v", err)
	}
	if string(text) != strings.TrimSuffix(string(release), "\n") {
		t.Errorf("InRelease signs %q, not the Release", text)
	}
	if _, err := keyring.VerifyDetached(release, read("Release.gpg")); err != nil {
		t.Errorf("Release.gpg: %v", err)
	}
	rel, err := ParseRelease(release)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

// checkHashes checks each file the Release lists, and its by-hash copy,
// against the Release.
func checkHashes(t *testing.T, dists string, rel *Release) {
	t.Helper()
	for name, f := range rel.Files {
		for _, file := range []string{filepath.Join(dists, filepath.FromSlash(name)), filepath.Join(dists, filepath.Dir(name), "by-hash", "SHA256", f.SHA256)} {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != f.SHA256 || int64(len(data)) != f.Size {
				t.Errorf("%s doesn't match the Release", file)
			}
		}
	}
}

// The same suite gives the same files, apart from the signatures.
func TestWriteIsDeterministic(t *testing.T) {
	suite, packages := testSuite(t)
	signer, _ := apttest.Key(t)
	var trees []map[string][]byte
	for range 2 {
		dir := t.TempDir()
		if err := Write(dir, suite, packages, signer); err != nil {
			t.Fatal(err)
		}
		tree := map[string][]byte{}
		err := filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() || e.Name() == "InRelease" || e.Name() == "Release.gpg" {
				return err
			}
			data, err := os.ReadFile(path)
			rel, _ := filepath.Rel(dir, path)
			tree[rel] = data
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		trees = append(trees, tree)
	}
	if len(trees[0]) != len(trees[1]) {
		t.Fatalf("%d and %d files", len(trees[0]), len(trees[1]))
	}
	for name, data := range trees[0] {
		if !bytes.Equal(data, trees[1][name]) {
			t.Errorf("%s differs", name)
		}
	}
}

func TestWithFilename(t *testing.T) {
	if got := withFilename("Package: curl\nSize: 10\n", "pool/x.deb"); got != "Package: curl\nSize: 10\nFilename: pool/x.deb\n" {
		t.Errorf("withFilename = %q", got)
	}
}

func TestWriteErrors(t *testing.T) {
	suite, packages := testSuite(t)
	signer, _ := apttest.Key(t)
	suite.Packages[0].SHA256 = strings.Repeat("00", 32)
	if err := Write(t.TempDir(), suite, packages, signer); err == nil || !strings.HasPrefix(err.Error(), "package curl 1.0: ") {
		t.Errorf("a package missing from the pool: %v", err)
	}
}
