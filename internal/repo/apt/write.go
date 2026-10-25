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
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ulikunitz/xz"

	"github.com/packtrix/packtrix/internal/pgp"
	"github.com/packtrix/packtrix/internal/pool"
)

// Published is a package of a repository that Packtrix publishes.
type Published struct {
	Name    string
	Version string
	// Architecture is the index the package goes into, such as "amd64";
	// a package of architecture "all" goes into each.
	Architecture string
	// Paragraph is the package's paragraph from the upstream index. Write
	// replaces its Filename.
	Paragraph string
	// Filename is where the file goes, relative to the repository's root,
	// such as "pool/updates/main/c/curl/curl_8.5.0_amd64.deb".
	Filename string
	// SHA256 is the file's name in the package pool.
	SHA256 string
}

// Suite is a repository that Packtrix publishes: one suite, with one
// component, main, which merges the packages of every upstream repository
// of a target.
type Suite struct {
	Origin   string
	Label    string
	Codename string
	// Architectures each get an index, even an empty one.
	Architectures []string
	// Date is written as the Release's date, unless it is zero.
	Date     time.Time
	Packages []Published
}

const component = "main"

var filenameRE = regexp.MustCompile(`(?m)^Filename: .*$`)

// Write writes a suite into dir:
//
//	pool/…                                   the package files, hard-linked from the pool
//	dists/<codename>/main/binary-<arch>/     Packages, Packages.gz, Packages.xz, by-hash/SHA256/…
//	dists/<codename>/Release                 the SHA-256 of each index, without Valid-Until
//	dists/<codename>/InRelease, Release.gpg  signed with signer
//
// The same suite always gives the same files, apart from the signatures.
func Write(dir string, s Suite, packages *pool.Pool, signer *pgp.Signer) error {
	linked := map[string]bool{}
	for _, p := range s.Packages {
		if linked[p.Filename] {
			continue
		}
		linked[p.Filename] = true
		if err := link(packages.Path(p.SHA256), filepath.Join(dir, filepath.FromSlash(p.Filename))); err != nil {
			return fmt.Errorf("package %s %s: %w", p.Name, p.Version, err)
		}
	}

	dists := path.Join("dists", s.Codename)
	var hashes strings.Builder
	for _, arch := range slices.Sorted(slices.Values(s.Architectures)) {
		var text strings.Builder
		for _, p := range sortedFor(s.Packages, arch) {
			text.WriteString(withFilename(p.Paragraph, p.Filename))
			text.WriteString("\n")
		}
		index := path.Join(component, "binary-"+arch, "Packages")
		for _, ext := range []string{"", ".gz", ".xz"} {
			data, err := compress(text.String(), ext)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			hash := hex.EncodeToString(sum[:])
			for _, name := range []string{index + ext, path.Join(path.Dir(index), "by-hash/SHA256", hash)} {
				if err := writeFile(filepath.Join(dir, dists, filepath.FromSlash(name)), data); err != nil {
					return err
				}
			}
			fmt.Fprintf(&hashes, " %s %d %s\n", hash, len(data), index+ext)
		}
	}

	var release strings.Builder
	fmt.Fprintf(&release, "Origin: %s\nLabel: %s\nSuite: %s\nCodename: %s\n", s.Origin, s.Label, s.Codename, s.Codename)
	if !s.Date.IsZero() {
		fmt.Fprintf(&release, "Date: %s\n", s.Date.UTC().Format(time.RFC1123))
	}
	fmt.Fprintf(&release, "Architectures: %s\nComponents: %s\nAcquire-By-Hash: yes\nSHA256:\n%s",
		strings.Join(slices.Sorted(slices.Values(s.Architectures)), " "), component, hashes.String())
	text := []byte(release.String())
	inRelease, err := signer.Clearsign(text)
	if err != nil {
		return err
	}
	signature, err := signer.DetachSign(text)
	if err != nil {
		return err
	}
	for name, data := range map[string][]byte{"Release": text, "InRelease": inRelease, "Release.gpg": signature} {
		if err := writeFile(filepath.Join(dir, dists, name), data); err != nil {
			return err
		}
	}
	return nil
}

// withFilename returns a paragraph whose Filename is filename.
func withFilename(paragraph, filename string) string {
	if filenameRE.MatchString(paragraph) {
		return filenameRE.ReplaceAllLiteralString(paragraph, "Filename: "+filename)
	}
	return strings.TrimSuffix(paragraph, "\n") + "\nFilename: " + filename + "\n"
}

// sortedFor returns the packages of an index, by name, then version.
func sortedFor(packages []Published, arch string) []Published {
	var out []Published
	for _, p := range packages {
		if p.Architecture == arch {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b Published) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.Version, b.Version))
	})
	return out
}

// link hard-links a pool file into the repository, or copies it when the
// two are on different file systems.
func link(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Link(src, dst); err == nil || os.IsExist(err) {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o444)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func writeFile(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	return os.WriteFile(name, data, 0o644)
}

// compress compresses an index as ext says. Both compressions give the same
// bytes for the same text: gzip's header holds no name or time.
func compress(text, ext string) ([]byte, error) {
	if ext == "" {
		return []byte(text), nil
	}
	var buf bytes.Buffer
	var w io.WriteCloser
	if ext == ".xz" {
		xw, err := xz.NewWriter(&buf)
		if err != nil {
			return nil, err
		}
		w = xw
	} else {
		w = gzip.NewWriter(&buf)
	}
	if _, err := io.WriteString(w, text); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
