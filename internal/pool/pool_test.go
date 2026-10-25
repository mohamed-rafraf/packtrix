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

package pool

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func open(t *testing.T) *Pool {
	t.Helper()
	p, err := Open(filepath.Join(t.TempDir(), "pool"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func sum(data string) string {
	s := sha256.Sum256([]byte(data))
	return hex.EncodeToString(s[:])
}

// files returns the files under the pool's directory.
func files(t *testing.T, p *Pool) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(p.Dir(), func(path string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			rel, _ := filepath.Rel(p.Dir(), path)
			out = append(out, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

const deb = "a .deb file"

func TestAdd(t *testing.T) {
	p := open(t)
	s := sum(deb)
	if p.Has(s) {
		t.Fatal("an empty pool has a file")
	}
	// The same content twice is stored once.
	for range 2 {
		if err := p.Add(strings.NewReader(deb), s); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := files(t, p), []string{"sha256/" + s[:2] + "/" + s}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("files = %q, want %q", got, want)
	}
	if !p.Has(s) || p.Path(s) != filepath.Join(p.Dir(), "sha256", s[:2], s) {
		t.Errorf("Has = %t, Path = %s", p.Has(s), p.Path(s))
	}
	info, err := os.Stat(p.Path(s))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o444 {
		t.Errorf("mode = %v, want read-only", info.Mode())
	}
	data, ok, err := p.Read(s)
	if err != nil || !ok || string(data) != deb {
		t.Errorf("Read = %q, %t, %v", data, ok, err)
	}
}

// A write that is interrupted, or whose content doesn't match, never leaves
// a file in the pool.
func TestAddRejects(t *testing.T) {
	p := open(t)
	s := sum(deb)
	for name, r := range map[string]io.Reader{
		"other content": strings.NewReader("another file"),
		"interrupted":   io.MultiReader(strings.NewReader(deb[:4]), iotest.ErrReader(errors.New("connection reset"))),
	} {
		if err := p.Add(r, s); err == nil {
			t.Errorf("%s: Add succeeded", name)
		}
	}
	if got := files(t, p); len(got) != 0 {
		t.Errorf("files = %q, want none", got)
	}
	if err := p.Add(strings.NewReader(deb), "ABC"); err == nil || err.Error() != `invalid SHA-256 "ABC"` {
		t.Errorf("invalid hash: error = %v", err)
	}
}

func TestCommit(t *testing.T) {
	p := open(t)
	s := sum(deb)
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(p.Partial(s), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// A download in progress isn't in the pool.
	write(deb[:4])
	if p.Has(s) {
		t.Error("a partial download is in the pool")
	}
	for _, tt := range []struct {
		content string
		size    int64
		want    string
	}{
		{"tampered!!!", int64(len(deb)), "got 11 bytes with SHA-256 " + sum("tampered!!!") + ", want 11 bytes with SHA-256 " + s},
		{deb, 5, "got 11 bytes with SHA-256 " + s + ", want 5 bytes with SHA-256 " + s},
	} {
		write(tt.content)
		if err := p.Commit(s, tt.size); err == nil || err.Error() != tt.want {
			t.Errorf("Commit = %v, want %q", err, tt.want)
		}
		// The bad download is gone, so the next one starts over.
		if _, err := os.Stat(p.Partial(s)); !os.IsNotExist(err) || p.Has(s) {
			t.Errorf("after a bad download: partial %v, in the pool %t", err, p.Has(s))
		}
	}

	write(deb)
	if err := p.Commit(s, -1); err != nil {
		t.Fatal(err)
	}
	if got, want := files(t, p), []string{"sha256/" + s[:2] + "/" + s}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("files = %q, want %q", got, want)
	}
	if err := p.Commit(sum("missing"), 1); err == nil {
		t.Error("committing a missing download succeeded")
	}
	if err := p.Commit("x", 1); err == nil {
		t.Error("committing an invalid hash succeeded")
	}
}

// A file changed behind Packtrix's back is removed when it is read.
func TestReadChecks(t *testing.T) {
	p := open(t)
	s := sum(deb)
	if err := p.Add(strings.NewReader(deb), s); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p.Path(s), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Path(s), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, ok, err := p.Read(s); ok || err != nil || data != nil {
		t.Errorf("Read = %q, %t, %v, want a missing file", data, ok, err)
	}
	if p.Has(s) {
		t.Error("the changed file is still in the pool")
	}
	if _, ok, err := p.Read(sum("missing")); ok || err != nil {
		t.Errorf("missing file: %t, %v", ok, err)
	}
	if _, _, err := p.Read("x"); err == nil {
		t.Error("reading an invalid hash succeeded")
	}
}

func TestPrune(t *testing.T) {
	p := open(t)
	old, used, current := sum("old"), sum("used"), sum("current")
	for content, s := range map[string]string{"old": old, "used": used, "current": current} {
		if err := p.Add(strings.NewReader(content), s); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(p.Partial(sum("abandoned")), []byte("aban"), 0o644); err != nil {
		t.Fatal(err)
	}
	month := time.Now().Add(-31 * 24 * time.Hour)
	for _, path := range []string{p.Path(old), p.Path(used), p.Partial(sum("abandoned"))} {
		if err := os.Chtimes(path, month, month); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Touch(used); err != nil {
		t.Fatal(err)
	}

	removed, err := p.Prune(time.Now().Add(-30 * 24 * time.Hour))
	if err != nil || removed != 2 {
		t.Errorf("Prune = %d, %v, want 2 removed", removed, err)
	}
	if p.Has(old) || !p.Has(used) || !p.Has(current) {
		t.Errorf("after Prune: old %t, used %t, current %t", p.Has(old), p.Has(used), p.Has(current))
	}
	if err := p.Touch("x"); err == nil {
		t.Error("touching an invalid hash succeeded")
	}
}

func TestOpenFails(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(file); err == nil {
		t.Error("Open on a file succeeded")
	}
}
