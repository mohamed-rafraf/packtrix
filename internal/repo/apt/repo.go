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

// Package apt reads upstream APT repositories and verifies them: the
// InRelease file against the repository's pinned keys, then each Packages
// index against the SHA-256 that the signed Release file gives.
package apt

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/ulikunitz/xz"

	"github.com/packtrix/packtrix/internal/deb"
	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/pgp"
)

// now is the time that Valid-Until is checked against. Tests change it.
var now = time.Now

// Source is an APT repository to read, such as one that the expand stage
// gives.
type Source struct {
	Name string
	URL  string
	// Suite is a suite, such as "noble-updates", or a path ending with "/"
	// for a flat repository.
	Suite      string
	Components []string
	// Keyring holds the keys that may sign the repository.
	Keyring *pgp.Keyring
	Auth    *fetch.Auth
}

// Repository is an upstream APT repository, read and verified for one
// architecture.
type Repository struct {
	// Name, URL and Suite are those of the repository that was read.
	Name  string
	URL   string
	Suite string
	// Release is the verified Release file.
	Release *Release
	// Signer is the fingerprint of the key that signed the Release.
	Signer  string
	Indexes []Index
}

// Index holds the packages of one Packages index.
type Index struct {
	// Component is empty in a flat repository.
	Component string
	// Architecture is the index's architecture, "all" for binary-all, or
	// empty in a flat repository, whose index lists every architecture.
	Architecture string
	Packages     []*deb.Package
}

// Packages returns the packages of every index.
func (r *Repository) Packages() []*deb.Package {
	var out []*deb.Package
	for _, ix := range r.Indexes {
		out = append(out, ix.Packages...)
	}
	return out
}

// Read fetches an APT repository's metadata for one architecture and
// verifies it:
//
//   - the InRelease file, or the Release and Release.gpg files when there
//     is no InRelease, against the repository's pinned keys. The Release
//     must be for the suite asked for, and not have expired;
//   - the Packages index of each component for the architecture, and for
//     "all" when the architecture's indexes don't include those packages,
//     against the size and SHA-256 that the Release gives. Indexes are
//     fetched by hash when the repository allows it, compressed with xz or
//     gzip when it offers them.
func Read(ctx context.Context, c *fetch.Client, src Source, arch string) (*Repository, error) {
	r := &Repository{Name: src.Name, URL: src.URL, Suite: src.Suite}
	if err := r.read(ctx, c, src, arch); err != nil {
		return nil, fmt.Errorf("repository %s (%s %s): %w", src.Name, src.URL, src.Suite, err)
	}
	return r, nil
}

// flat reports whether a suite is that of a flat repository, such as "/".
func flat(suite string) bool {
	return strings.HasSuffix(suite, "/")
}

func (r *Repository) read(ctx context.Context, c *fetch.Client, repo Source, arch string) error {
	auth := repo.Auth
	dir := strings.TrimSuffix(repo.URL, "/") + "/dists/" + repo.Suite + "/"
	if flat(repo.Suite) {
		dir = strings.TrimSuffix(repo.URL, "/") + "/" + strings.TrimPrefix(strings.TrimPrefix(repo.Suite, "."), "/")
	}

	text, err := r.readRelease(ctx, c, repo, dir)
	if err != nil {
		return err
	}
	if r.Release, err = ParseRelease(text); err != nil {
		return err
	}
	rel := r.Release
	if !flat(repo.Suite) && repo.Suite != rel.Suite && repo.Suite != rel.Codename {
		return fmt.Errorf("the Release file is for suite %q (%s), not %q", rel.Suite, rel.Codename, repo.Suite)
	}
	if !rel.ValidUntil.IsZero() && now().After(rel.ValidUntil) {
		return fmt.Errorf("the Release file expired on %s: the server, or a mirror on the way, serves outdated metadata", rel.ValidUntil.Format(time.RFC1123))
	}

	if flat(repo.Suite) {
		packages, err := r.readIndex(ctx, c, auth, dir, "Packages")
		if err != nil {
			return err
		}
		r.Indexes = append(r.Indexes, Index{Packages: packages})
		return nil
	}
	for _, component := range repo.Components {
		archs := []string{arch}
		if !rel.AllInArchIndexes && listed(rel, component+"/binary-all/Packages") {
			archs = append(archs, "all")
		}
		for _, a := range archs {
			packages, err := r.readIndex(ctx, c, auth, dir, component+"/binary-"+a+"/Packages")
			if err != nil {
				return err
			}
			r.Indexes = append(r.Indexes, Index{Component: component, Architecture: a, Packages: packages})
		}
	}
	return nil
}

// readRelease fetches and verifies the Release file. It only falls back to
// Release and Release.gpg when there is no InRelease, never when the
// InRelease is invalid.
func (r *Repository) readRelease(ctx context.Context, c *fetch.Client, repo Source, dir string) ([]byte, error) {
	keyring, auth := repo.Keyring, repo.Auth
	inRelease, err := c.Get(ctx, dir+"InRelease", auth)
	if err == nil {
		text, signer, err := keyring.VerifyClearsigned(inRelease)
		if err != nil {
			return nil, fmt.Errorf("InRelease: %w", err)
		}
		r.Signer = signer
		return text, nil
	}
	if !fetch.IsNotFound(err) {
		return nil, err
	}
	release, err := c.Get(ctx, dir+"Release", auth)
	if err != nil {
		return nil, err
	}
	signature, err := c.Get(ctx, dir+"Release.gpg", auth)
	if err != nil {
		return nil, err
	}
	if r.Signer, err = keyring.VerifyDetached(release, signature); err != nil {
		return nil, fmt.Errorf("checking Release.gpg: %w", err)
	}
	return release, nil
}

// compressions are the forms of an index, in order of preference.
var compressions = []string{".xz", ".gz", ""}

func listed(rel *Release, index string) bool {
	for _, ext := range compressions {
		if _, ok := rel.Files[index+ext]; ok {
			return true
		}
	}
	return false
}

// readIndex fetches a Packages index, checks it against the Release, and
// reads its packages.
func (r *Repository) readIndex(ctx context.Context, c *fetch.Client, auth *fetch.Auth, dir, index string) ([]*deb.Package, error) {
	for _, ext := range compressions {
		file, ok := r.Release.Files[index+ext]
		if !ok {
			continue
		}
		name := index + ext
		data, err := r.fetchFile(ctx, c, auth, dir, name, file)
		if err != nil {
			return nil, err
		}
		packages, err := parseIndex(data, ext)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return packages, nil
	}
	return nil, fmt.Errorf("the Release file lists no %s index", index)
}

// fetchFile fetches a file that the Release lists, by hash when the
// repository allows it, and checks its size and SHA-256.
func (r *Repository) fetchFile(ctx context.Context, c *fetch.Client, auth *fetch.Auth, dir, name string, want File) ([]byte, error) {
	var data []byte
	var err error
	if r.Release.AcquireByHash {
		data, err = c.Get(ctx, dir+path.Join(path.Dir(name), "by-hash/SHA256", want.SHA256), auth)
	}
	if !r.Release.AcquireByHash || fetch.IsNotFound(err) {
		data, err = c.Get(ctx, dir+name, auth)
	}
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); int64(len(data)) != want.Size || got != want.SHA256 {
		return nil, fmt.Errorf("%s doesn't match the signed Release file: got %d bytes with SHA-256 %s, want %d bytes with SHA-256 %s", name, len(data), got, want.Size, want.SHA256)
	}
	return data, nil
}

func parseIndex(data []byte, ext string) ([]*deb.Package, error) {
	var r io.Reader = bytes.NewReader(data)
	switch ext {
	case ".xz":
		xr, err := xz.NewReader(r)
		if err != nil {
			return nil, err
		}
		r = xr
	case ".gz":
		gr, err := gzip.NewReader(r)
		if err != nil {
			return nil, err
		}
		r = gr
	}
	var packages []*deb.Package
	err := deb.ReadParagraphs(r, func(p deb.Paragraph) error {
		pkg, err := deb.ParsePackage(p)
		if err != nil {
			return err
		}
		packages = append(packages, pkg)
		return nil
	})
	return packages, err
}

// CheckSameRelease returns an error when repositories disagree on the
// distribution or the release, such as Ubuntu 22.04 and 24.04: the resolver
// would pick packages built for the newer release for every machine. The
// suites of one release, such as bookworm and bookworm-security, agree.
func CheckSameRelease(repos []*Repository) error {
	for _, r := range repos[min(1, len(repos)):] {
		if got, want := r.Release.Distribution(), repos[0].Release.Distribution(); got != want {
			return fmt.Errorf("repositories %s and %s are for different releases: %s and %s; a target can't mix releases", repos[0].Name, r.Name, want, got)
		}
	}
	return nil
}
