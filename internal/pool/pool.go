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

// Package pool stores files by their SHA-256: each file once, however many
// targets and snapshots use it. A file only reaches its place in the pool
// once its content matches its hash, so a file in the pool is always whole
// and right.
//
// Packtrix keeps two pools: the package pool, next to the published
// snapshots, and the cache of upstream indexes.
package pool

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Pool is a directory of files named by their SHA-256:
//
//	sha256/ab/abcdef…   the files, read-only
//	partial/abcdef…     downloads in progress
type Pool struct {
	dir string
}

// Open returns the pool in dir, creating it if needed.
func Open(dir string) (*Pool, error) {
	for _, d := range []string{"sha256", "partial"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return nil, err
		}
	}
	return &Pool{dir: dir}, nil
}

// Dir returns the pool's directory.
func (p *Pool) Dir() string {
	return p.dir
}

func checkSum(sum string) error {
	if !sha256RE.MatchString(sum) {
		return fmt.Errorf("invalid SHA-256 %q", sum)
	}
	return nil
}

// Path returns where the file with this SHA-256 is stored, or would be.
func (p *Pool) Path(sum string) string {
	return filepath.Join(p.dir, "sha256", sum[:min(2, len(sum))], sum)
}

// Has reports whether the pool holds the file with this SHA-256.
func (p *Pool) Has(sum string) bool {
	_, err := os.Stat(p.Path(sum))
	return checkSum(sum) == nil && err == nil
}

// Read returns the file with this SHA-256, after checking that its content
// still matches: a file changed behind Packtrix's back is removed, and
// reported as missing.
func (p *Pool) Read(sum string) ([]byte, bool, error) {
	if err := checkSum(sum); err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(p.Path(sum))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != sum {
		return nil, false, os.Remove(p.Path(sum))
	}
	return data, true, nil
}

// Add stores what r reads, if its SHA-256 is sum. Otherwise it stores
// nothing and returns an error.
func (p *Pool) Add(r io.Reader, sum string) error {
	if err := checkSum(sum); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Join(p.dir, "partial"), "add-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), r)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		return fmt.Errorf("got %d bytes with SHA-256 %s, want SHA-256 %s", n, got, sum)
	}
	return p.place(f.Name(), sum)
}

// Partial returns where the file with this SHA-256 is downloaded before it
// is checked. It is the same in every run, so a later run resumes a
// download that was interrupted. Only one download of a file may run at a
// time.
func (p *Pool) Partial(sum string) string {
	return filepath.Join(p.dir, "partial", sum)
}

// Commit checks the downloaded file at Partial(sum) against its SHA-256 and
// its size, unless size is negative, and moves it into the pool. A file
// that doesn't match is removed, so the next download starts over.
func (p *Pool) Commit(sum string, size int64) error {
	if err := checkSum(sum); err != nil {
		return err
	}
	partial := p.Partial(sum)
	f, err := os.Open(partial)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum || size >= 0 && n != size {
		if err := os.Remove(partial); err != nil {
			return err
		}
		return fmt.Errorf("got %d bytes with SHA-256 %s, want %d bytes with SHA-256 %s", n, got, size, sum)
	}
	return p.place(partial, sum)
}

// place moves a checked file into the pool, read-only, in one step.
func (p *Pool) place(file, sum string) error {
	dst := p.Path(sum)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Chmod(file, 0o444); err != nil {
		return err
	}
	return os.Rename(file, dst)
}

// Touch records that the file with this SHA-256 was used now, so that Prune
// keeps it.
func (p *Pool) Touch(sum string) error {
	if err := checkSum(sum); err != nil {
		return err
	}
	now := time.Now()
	return os.Chtimes(p.Path(sum), now, now)
}

// Prune removes the files last used before t, and the downloads in
// progress last written before t, and returns how many it removed.
func (p *Pool) Prune(before time.Time) (int, error) {
	removed := 0
	for _, d := range []string{"sha256", "partial"} {
		err := filepath.WalkDir(filepath.Join(p.dir, d), func(path string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return err
			}
			info, err := e.Info()
			if err != nil {
				return err
			}
			if info.ModTime().Before(before) {
				if err := os.Remove(path); err != nil {
					return err
				}
				removed++
			}
			return nil
		})
		if err != nil {
			return removed, err
		}
	}
	return removed, nil
}
