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

// Package apttest builds signed APT repositories on disk and serves them,
// for tests.
package apttest

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
	"github.com/ulikunitz/xz"

	"github.com/packtrix/packtrix/internal/pgp"
)

// PrivateKey generates a signing key and returns it armored, as
// publish.signing.key references it. The key is made an hour ago: a clock
// that steps back a little, as virtual machines' clocks do, would otherwise
// make it look made in the future, and not valid yet.
func PrivateKey(t testing.TB) []byte {
	t.Helper()
	created := time.Now().Add(-time.Hour)
	e, err := openpgp.NewEntity("Test Archive Automatic Signing Key", "", "archive@example.org",
		&packet.Config{Algorithm: packet.PubKeyAlgoEdDSA, Time: func() time.Time { return created }})
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
	return buf.Bytes()
}

// Key generates a signing key, like an archive's, and returns a signer and
// a keyring that pins it.
func Key(t testing.TB) (*pgp.Signer, *pgp.Keyring) {
	t.Helper()
	s, err := pgp.LoadSigner(PrivateKey(t))
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

// APT is the apt-get of the machine running the tests, with its own state
// in a temporary directory, and only one source entry, as a machine that
// uses a Packtrix repository has.
type APT struct {
	t     testing.TB
	bin   string
	state string
}

// NewAPT returns the machine's apt-get with this source entry, or skips the
// test when apt-get isn't installed.
func NewAPT(t testing.TB, sources string) *APT {
	t.Helper()
	bin, err := exec.LookPath("apt-get")
	if err != nil {
		t.Skip("apt-get isn't installed")
	}
	a := &APT{t: t, bin: bin, state: t.TempDir()}
	for _, d := range []string{"sources.list.d", "trusted.gpg.d", "lists/partial", "archives/partial", "download"} {
		if err := os.MkdirAll(filepath.Join(a.state, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(a.state, "sources.list.d", "packtrix.sources"), []byte(sources), 0o644); err != nil {
		t.Fatal(err)
	}
	return a
}

// Run runs apt-get with these arguments, in the directory that apt-get
// download writes to, and returns its output. Warnings and errors fail the
// test.
func (a *APT) Run(args ...string) string {
	a.t.Helper()
	options := []string{
		"-o", "Dir::Etc::SourceList=/dev/null",
		"-o", "Dir::Etc::SourceParts=" + filepath.Join(a.state, "sources.list.d"),
		"-o", "Dir::Etc::Trusted=/dev/null",
		"-o", "Dir::Etc::TrustedParts=" + filepath.Join(a.state, "trusted.gpg.d"),
		"-o", "Dir::State::Lists=" + filepath.Join(a.state, "lists"),
		"-o", "Dir::Cache=" + a.state,
		"-o", "Dir::Cache::Archives=" + filepath.Join(a.state, "archives"),
		"-o", "Debug::NoLocking=1",
	}
	cmd := exec.CommandContext(a.t.Context(), a.bin, append(options, args...)...)
	cmd.Dir = a.DownloadDir()
	out, err := cmd.CombinedOutput()
	if err != nil {
		a.t.Fatalf("apt-get %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "W:") || strings.HasPrefix(line, "E:") || strings.HasPrefix(line, "N:") {
			a.t.Errorf("apt-get %s: %s", strings.Join(args, " "), line)
		}
	}
	return string(out)
}

// DownloadDir returns where apt-get download writes.
func (a *APT) DownloadDir() string {
	return filepath.Join(a.state, "download")
}

// Archive is an APT archive on disk, served over HTTP or HTTPS. It records
// the paths it is asked for.
type Archive struct {
	t      testing.TB
	root   string
	signer *pgp.Signer
	server *httptest.Server

	mu       sync.Mutex
	requests []string
}

// New serves an empty archive whose Release files signer signs, over HTTPS
// when secure is set.
func New(t testing.TB, signer *pgp.Signer, secure bool) *Archive {
	t.Helper()
	a := &Archive{t: t, root: t.TempDir(), signer: signer}
	files := http.FileServer(http.Dir(a.root))
	a.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.requests = append(a.requests, r.URL.Path)
		a.mu.Unlock()
		files.ServeHTTP(w, r)
	}))
	a.server.Config.ErrorLog = log.New(io.Discard, "", 0)
	if secure {
		a.server.StartTLS()
	} else {
		a.server.Start()
	}
	t.Cleanup(a.server.Close)
	return a
}

// URL returns the archive's root URL, without a trailing slash.
func (a *Archive) URL() string {
	return a.server.URL
}

// CAFile writes the certificate of an HTTPS archive to a file, for clients
// to trust.
func (a *Archive) CAFile() string {
	a.t.Helper()
	file := filepath.Join(a.t.TempDir(), "ca.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.server.Certificate().Raw})
	if err := os.WriteFile(file, data, 0o600); err != nil {
		a.t.Fatal(err)
	}
	return file
}

// Requests returns the paths asked for since the last ResetRequests.
func (a *Archive) Requests() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.requests)
}

// ResetRequests forgets the paths asked for so far.
func (a *Archive) ResetRequests() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests = nil
}

// Write writes a file of the archive, by its path from the root.
func (a *Archive) Write(name string, data []byte) {
	a.t.Helper()
	file := filepath.Join(a.root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		a.t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		a.t.Fatal(err)
	}
}

// Read reads a file of the archive, by its path from the root.
func (a *Archive) Read(name string) []byte {
	a.t.Helper()
	data, err := os.ReadFile(filepath.Join(a.root, filepath.FromSlash(name)))
	if err != nil {
		a.t.Fatal(err)
	}
	return data
}

// Suite describes a Release file and its indexes.
type Suite struct {
	// Dir is the Release's directory from the root, such as
	// "ubuntu/dists/noble", or the repository's root for a flat repository.
	Dir string
	// Fields are the Release's fields before its SHA256 hashes.
	Fields string
	// Indexes are Packages contents, by path without extension, such as
	// "main/binary-amd64/Packages".
	Indexes map[string]string
	// Compressions are the forms each index is published in: ".xz",
	// ".gz", "".
	Compressions []string
	// ByHash also publishes each index under by-hash/SHA256/.
	ByHash bool
	// Detached signs the Release as Release.gpg instead of InRelease.
	Detached bool
}

// Publish writes a suite's indexes and its signed Release.
func (a *Archive) Publish(s Suite) {
	a.t.Helper()
	var hashes strings.Builder
	var indexes []string
	for index := range s.Indexes {
		indexes = append(indexes, index)
	}
	slices.Sort(indexes)
	for _, index := range indexes {
		for _, ext := range s.Compressions {
			data := Compress(a.t, s.Indexes[index], ext)
			sum := sha256.Sum256(data)
			hash := hex.EncodeToString(sum[:])
			a.Write(path.Join(s.Dir, index+ext), data)
			if s.ByHash {
				a.Write(path.Join(s.Dir, path.Dir(index), "by-hash/SHA256", hash), data)
			}
			fmt.Fprintf(&hashes, " %s %d %s\n", hash, len(data), index+ext)
		}
	}
	release := []byte(s.Fields + "SHA256:\n" + hashes.String())
	if s.Detached {
		sig, err := a.signer.DetachSign(release)
		if err != nil {
			a.t.Fatal(err)
		}
		a.Write(path.Join(s.Dir, "Release"), release)
		a.Write(path.Join(s.Dir, "Release.gpg"), sig)
		return
	}
	inRelease, err := a.signer.Clearsign(release)
	if err != nil {
		a.t.Fatal(err)
	}
	a.Write(path.Join(s.Dir, "InRelease"), inRelease)
}

// Compress compresses text as ext says: ".xz", ".gz", or "" for none.
func Compress(t testing.TB, text, ext string) []byte {
	t.Helper()
	var buf bytes.Buffer
	var w io.WriteCloser
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
