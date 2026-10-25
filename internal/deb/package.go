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

package deb

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

var sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Package is a binary package of a Packages index, with the fields that
// dependency resolution needs.
type Package struct {
	Name         string
	Version      Version
	Architecture string
	// Essential is set for packages that every system has, such as dpkg.
	Essential bool
	// Priority is "required", "important", "standard", "optional" or
	// "extra", or empty.
	Priority   string
	Provides   []Relation
	PreDepends []Alternatives
	Depends    []Alternatives
	Recommends []Alternatives
	Breaks     []Alternatives
	Conflicts  []Alternatives
	// Filename is the path of the .deb, relative to the repository's base
	// URL, such as "pool/main/c/curl/curl_8.5.0-2ubuntu10.6_amd64.deb".
	Filename string
	// Size and SHA256 describe the .deb.
	Size   int64
	SHA256 string
}

// String returns name=version, as apt-get install accepts it.
func (p *Package) String() string {
	return p.Name + "=" + p.Version.String()
}

// ParsePackage reads a package from its paragraph of a Packages index.
func ParsePackage(p Paragraph) (*Package, error) {
	pkg := &Package{
		Name:         p.Get("Package"),
		Architecture: p.Get("Architecture"),
		Essential:    p.Get("Essential") == "yes",
		Priority:     p.Get("Priority"),
		Filename:     p.Get("Filename"),
		SHA256:       p.Get("SHA256"),
	}
	if pkg.Name == "" {
		return nil, errors.New("paragraph without a Package field")
	}
	if !packageNameRE.MatchString(pkg.Name) {
		return nil, fmt.Errorf("invalid package name %q", pkg.Name)
	}
	fail := func(field string, err error) (*Package, error) {
		return nil, fmt.Errorf("package %s: %s: %w", pkg.Name, field, err)
	}
	if !archRE.MatchString(pkg.Architecture) {
		return fail("Architecture", fmt.Errorf("invalid architecture %q", pkg.Architecture))
	}
	var err error
	if pkg.Version, err = ParseVersion(p.Get("Version")); err != nil {
		return fail("Version", err)
	}
	if s := p.Get("Size"); s != "" {
		if pkg.Size, err = strconv.ParseInt(s, 10, 64); err != nil || pkg.Size < 0 {
			return fail("Size", fmt.Errorf("invalid size %q", s))
		}
	}
	if pkg.SHA256 != "" && !sha256RE.MatchString(pkg.SHA256) {
		return fail("SHA256", fmt.Errorf("invalid SHA-256 %q", pkg.SHA256))
	}
	if pkg.Provides, err = ParseProvides(p.Get("Provides")); err != nil {
		return fail("Provides", err)
	}
	for _, f := range []struct {
		name string
		deps *[]Alternatives
	}{
		{"Pre-Depends", &pkg.PreDepends},
		{"Depends", &pkg.Depends},
		{"Recommends", &pkg.Recommends},
		{"Breaks", &pkg.Breaks},
		{"Conflicts", &pkg.Conflicts},
	} {
		if *f.deps, err = ParseDependencies(p.Get(f.name)); err != nil {
			return fail(f.name, err)
		}
	}
	return pkg, nil
}
