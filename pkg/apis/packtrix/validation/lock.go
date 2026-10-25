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

package validation

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/packtrix/packtrix/pkg/apis/packtrix"
)

var (
	digestRE      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	sha256RE      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	fingerprintRE = regexp.MustCompile(`^([0-9A-F]{40}|[0-9A-F]{64})$`)
)

// ValidateLock checks a lock and returns a ValidationError listing every
// problem, or nil. Plan writes valid locks; this catches locks edited by
// hand or damaged.
func ValidateLock(l *packtrix.Lock) error {
	v := &validator{}
	s := &l.Spec
	v.match("spec.manifest", s.Manifest, digestRE, `a digest, "sha256:<hex>"`)
	if s.Catalog == "" {
		v.add("spec.catalog", "required")
	}
	type key struct{ name, arch string }
	var seen []key
	for i, t := range s.Targets {
		f := fmt.Sprintf("spec.targets[%d]", i)
		v.name(f+".name", t.Name)
		switch {
		case !slices.Contains(packtrix.SupportedArchitectures, t.Architecture):
			v.add(f+".architecture", "unsupported architecture %q", t.Architecture)
		case slices.Contains(seen, key{t.Name, t.Architecture}):
			v.add(f, "duplicate target %s (%s)", t.Name, t.Architecture)
		}
		seen = append(seen, key{t.Name, t.Architecture})
		if t.Format != packtrix.RepoTypeAPT && t.Format != packtrix.RepoTypeRPM {
			v.add(f+".format", "must be apt or rpm, got %q", t.Format)
		}

		var repos []string
		for j, r := range t.Repositories {
			rf := fmt.Sprintf("%s.repositories[%d]", f, j)
			v.name(rf+".name", r.Name)
			repos = append(repos, r.Name)
			v.url(rf+".url", r.URL)
			v.match(rf+".release", r.Release, digestRE, `a digest, "sha256:<hex>"`)
			v.match(rf+".signer", r.Signer, fingerprintRE, "a key fingerprint")
		}
		for j, p := range t.Packages {
			pf := fmt.Sprintf("%s.packages[%d]", f, j)
			switch {
			case p.Name == "":
				v.add(pf+".name", "required")
			case !packageNameRE.MatchString(p.Name):
				v.add(pf+".name", "invalid package name %q", p.Name)
			}
			if p.Version == "" {
				v.add(pf+".version", "required")
			}
			if p.Architecture == "" {
				v.add(pf+".architecture", "required")
			}
			if !slices.Contains(repos, p.Repository) {
				v.add(pf+".repository", "unknown repository %q", p.Repository)
			}
			v.url(pf+".url", p.URL)
			v.match(pf+".sha256", p.SHA256, sha256RE, "a SHA-256 in hex")
			if p.Size < 0 {
				v.add(pf+".size", "must not be negative")
			}
			if p.Epoch < 0 {
				v.add(pf+".epoch", "must not be negative")
			}
		}
	}
	if len(v.errs) == 0 {
		return nil
	}
	return v.errs
}

func (v *validator) match(field, value string, re *regexp.Regexp, what string) {
	if !re.MatchString(value) {
		v.add(field, "must be %s, got %q", what, value)
	}
}
