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
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/packtrix/packtrix/internal/deb"
)

// Release is the Release file of an APT repository: what the repository is,
// and the size and SHA-256 of each of its indexes.
type Release struct {
	Origin        string
	Label         string
	Suite         string
	Codename      string
	Version       string
	Architectures []string
	Components    []string
	// AcquireByHash is set when indexes can be fetched by their hash, which
	// stays consistent while the repository changes.
	AcquireByHash bool
	// AllInArchIndexes is set when the index of each architecture also
	// lists the packages of architecture "all"
	// (No-Support-for-Architecture-all: Packages).
	AllInArchIndexes bool
	// ValidUntil is when the Release expires, or zero.
	ValidUntil time.Time
	// Files are the files the Release vouches for, by path relative to its
	// directory, such as "main/binary-amd64/Packages.xz".
	Files map[string]File
}

// File is the size and SHA-256 that a Release gives for a file.
type File struct {
	Size   int64
	SHA256 string
}

var sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ParseRelease reads the text of a Release file. Only SHA-256 hashes count:
// apt doesn't trust the weaker ones either.
func ParseRelease(text []byte) (*Release, error) {
	var paragraphs []deb.Paragraph
	if err := deb.ReadParagraphs(bytes.NewReader(text), func(p deb.Paragraph) error {
		paragraphs = append(paragraphs, p)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("reading the Release file: %w", err)
	}
	if len(paragraphs) != 1 {
		return nil, fmt.Errorf("the Release file has %d paragraphs, not 1", len(paragraphs))
	}
	p := paragraphs[0]
	r := &Release{
		Origin:           p.Get("Origin"),
		Label:            p.Get("Label"),
		Suite:            p.Get("Suite"),
		Codename:         p.Get("Codename"),
		Version:          p.Get("Version"),
		Architectures:    strings.Fields(p.Get("Architectures")),
		Components:       strings.Fields(p.Get("Components")),
		AcquireByHash:    p.Get("Acquire-By-Hash") == "yes",
		AllInArchIndexes: p.Get("No-Support-for-Architecture-all") == "Packages",
		Files:            map[string]File{},
	}
	if s := p.Get("Valid-Until"); s != "" {
		t, err := time.Parse(time.RFC1123, s)
		if err != nil {
			return nil, fmt.Errorf("the Release file's Valid-Until %q isn't a date", s)
		}
		r.ValidUntil = t
	}
	hashes := p.Get("SHA256")
	if strings.TrimSpace(hashes) == "" {
		return nil, errors.New("the Release file has no SHA256 hashes")
	}
	for line := range strings.Lines(hashes) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 3 || !sha256RE.MatchString(fields[0]) {
			return nil, fmt.Errorf("the Release file has an invalid SHA256 line %q", strings.TrimSpace(line))
		}
		size, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("the Release file has an invalid size in %q", strings.TrimSpace(line))
		}
		r.Files[fields[2]] = File{Size: size, SHA256: fields[0]}
	}
	return r, nil
}

// Distribution returns the distribution and release the Release belongs to,
// such as "Debian bookworm": the codename without the suffix of suites such
// as bookworm-security.
func (r *Release) Distribution() string {
	codename, _, _ := strings.Cut(r.Codename, "-")
	return strings.TrimSpace(r.Origin + " " + codename)
}
