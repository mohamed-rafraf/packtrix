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
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/ulikunitz/xz"

	"github.com/packtrix/packtrix/internal/deb"
	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/pgp"
	"github.com/packtrix/packtrix/internal/pool"
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

// Reader reads APT repositories.
type Reader struct {
	Client *fetch.Client
	// Cache, if set, keeps the verified indexes across runs: an index that
	// a Release lists with a SHA-256 the cache holds isn't fetched again.
	// The Release itself is always fetched, since it says what is current.
	Cache *pool.Pool
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
	// Digest is the SHA-256 of the verified Release file's text, as
	// "sha256:<hex>". It changes whenever the repository does.
	Digest string
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
func (rd *Reader) Read(ctx context.Context, src Source, arch string) (*Repository, error) {
	r := &Repository{Name: src.Name, URL: src.URL, Suite: src.Suite}
	if err := r.read(ctx, rd, src, arch); err != nil {
		return nil, fmt.Errorf("repository %s (%s %s): %w", src.Name, src.URL, src.Suite, err)
	}
	return r, nil
}

// flat reports whether a suite is that of a flat repository, such as "/".
func flat(suite string) bool {
	return strings.HasSuffix(suite, "/")
}

// releaseDir returns the URL of the directory of a repository's Release.
func releaseDir(src Source) string {
	if flat(src.Suite) {
		return strings.TrimSuffix(src.URL, "/") + "/" + strings.TrimPrefix(strings.TrimPrefix(src.Suite, "."), "/")
	}
	return strings.TrimSuffix(src.URL, "/") + "/dists/" + src.Suite + "/"
}

// indexRef is a Packages index to read, without its compression.
type indexRef struct {
	component, arch, path string
}

// indexes returns the Packages indexes of a repository for an architecture:
// each component's, plus binary-all where the architecture's indexes don't
// include its packages.
func indexes(rel *Release, src Source, arch string) []indexRef {
	if flat(src.Suite) {
		return []indexRef{{path: "Packages"}}
	}
	var out []indexRef
	for _, component := range src.Components {
		archs := []string{arch}
		if !rel.AllInArchIndexes && listed(rel, component+"/binary-all/Packages") {
			archs = append(archs, "all")
		}
		for _, a := range archs {
			out = append(out, indexRef{component: component, arch: a, path: component + "/binary-" + a + "/Packages"})
		}
	}
	return out
}

func (r *Repository) read(ctx context.Context, rd *Reader, repo Source, arch string) error {
	dir := releaseDir(repo)
	text, err := r.readRelease(ctx, rd.Client, repo, dir)
	if err != nil {
		return err
	}
	if r.Release, err = ParseRelease(text); err != nil {
		return err
	}
	sum := sha256.Sum256(text)
	r.Digest = "sha256:" + hex.EncodeToString(sum[:])
	rel := r.Release
	if !flat(repo.Suite) && repo.Suite != rel.Suite && repo.Suite != rel.Codename {
		return fmt.Errorf("the Release file is for suite %q (%s), not %q", rel.Suite, rel.Codename, repo.Suite)
	}
	if !rel.ValidUntil.IsZero() && now().After(rel.ValidUntil) {
		return fmt.Errorf("the Release file expired on %s: the server, or a mirror on the way, serves outdated metadata", rel.ValidUntil.Format(time.RFC1123))
	}
	if rd.Cache != nil {
		// Building the published repository reads it again, by its digest.
		_ = rd.Cache.Add(bytes.NewReader(text), hex.EncodeToString(sum[:]))
	}

	for _, ix := range indexes(rel, repo, arch) {
		var packages []*deb.Package
		err := r.eachParagraph(ctx, rd, repo.Auth, dir, ix.path, false, func(p deb.Paragraph) error {
			pkg, err := deb.ParsePackage(p)
			if err != nil {
				return err
			}
			packages = append(packages, pkg)
			return nil
		})
		if err != nil {
			return err
		}
		r.Indexes = append(r.Indexes, Index{Component: ix.component, Architecture: ix.arch, Packages: packages})
	}
	return nil
}

// Key identifies a package of an index.
type Key struct {
	Name, Version, Architecture string
}

// Paragraphs returns the text of the wanted packages' paragraphs, as the
// repository had them when its Release file had this digest,
// "sha256:<hex>". The Release and its indexes normally come from the cache,
// where plan left them. Without them, Paragraphs fetches the repository
// again, which works as long as it hasn't changed since. It also returns
// the Release.
func (rd *Reader) Paragraphs(ctx context.Context, src Source, arch, digest string, want map[Key]bool) (map[Key]string, *Release, error) {
	r := &Repository{Name: src.Name, URL: src.URL, Suite: src.Suite}
	found, err := r.paragraphs(ctx, rd, src, arch, digest, want)
	if err != nil {
		return nil, nil, fmt.Errorf("repository %s (%s %s): %w", src.Name, src.URL, src.Suite, err)
	}
	return found, r.Release, nil
}

func (r *Repository) paragraphs(ctx context.Context, rd *Reader, src Source, arch, digest string, want map[Key]bool) (map[Key]string, error) {
	dir := releaseDir(src)
	sum := strings.TrimPrefix(digest, "sha256:")
	var text []byte
	if rd.Cache != nil {
		if data, ok, err := rd.Cache.Read(sum); err == nil && ok {
			text = data
		}
	}
	if text == nil {
		t, err := r.readRelease(ctx, rd.Client, src, dir)
		if err != nil {
			return nil, err
		}
		if got := sha256.Sum256(t); hex.EncodeToString(got[:]) != sum {
			return nil, errors.New("the repository changed since the lock was planned, and the cache doesn't hold it as it was; run packtrix plan")
		}
		text = t
	}
	var err error
	if r.Release, err = ParseRelease(text); err != nil {
		return nil, err
	}

	found := map[Key]string{}
	for _, ix := range indexes(r.Release, src, arch) {
		err := r.eachParagraph(ctx, rd, src.Auth, dir, ix.path, true, func(p deb.Paragraph) error {
			v, err := deb.ParseVersion(p.Get("Version"))
			if err != nil {
				return fmt.Errorf("package %s: %w", p.Get("Package"), err)
			}
			if k := (Key{p.Get("Package"), v.String(), p.Get("Architecture")}); want[k] {
				found[k] = p.Raw()
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return found, nil
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

// eachParagraph fetches a Packages index, checks it against the Release,
// and calls fn with each of its paragraphs, with their text when raw is
// set.
func (r *Repository) eachParagraph(ctx context.Context, rd *Reader, auth *fetch.Auth, dir, index string, raw bool, fn func(deb.Paragraph) error) error {
	for _, ext := range compressions {
		file, ok := r.Release.Files[index+ext]
		if !ok {
			continue
		}
		name := index + ext
		data, err := r.fetchFile(ctx, rd, auth, dir, name, file)
		if err != nil {
			return err
		}
		text, err := decompress(data, ext)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		read := deb.ReadParagraphs
		if raw {
			read = deb.ReadRawParagraphs
		}
		if err := read(text, fn); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	}
	return fmt.Errorf("the Release file lists no %s index", index)
}

// fetchFile fetches a file that the Release lists, by hash when the
// repository allows it, and checks its size and SHA-256. A file the cache
// holds isn't fetched again.
func (r *Repository) fetchFile(ctx context.Context, rd *Reader, auth *fetch.Auth, dir, name string, want File) ([]byte, error) {
	if rd.Cache != nil {
		// The cache checks the content against the hash as it reads.
		if data, ok, err := rd.Cache.Read(want.SHA256); err == nil && ok && int64(len(data)) == want.Size {
			_ = rd.Cache.Touch(want.SHA256)
			return data, nil
		}
	}
	c := rd.Client
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
	if rd.Cache != nil {
		// A cache that can't be written only costs a download next time.
		_ = rd.Cache.Add(bytes.NewReader(data), want.SHA256)
	}
	return data, nil
}

// decompress returns the text of an index compressed as ext says.
func decompress(data []byte, ext string) (io.Reader, error) {
	r := bytes.NewReader(data)
	switch ext {
	case ".xz":
		return xz.NewReader(r)
	case ".gz":
		return gzip.NewReader(r)
	default:
		return r, nil
	}
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
