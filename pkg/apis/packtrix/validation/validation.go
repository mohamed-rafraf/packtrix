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

// Package validation checks manifests in the internal API.
package validation

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/packtrix/packtrix/pkg/apis/packtrix"
)

// FieldError describes one invalid field.
type FieldError struct {
	// Field is the path of the field, e.g. "spec.targets[1].distro".
	Field  string
	Detail string
}

func (e FieldError) Error() string { return e.Field + ": " + e.Detail }

// ValidationError lists every problem found in a manifest.
type ValidationError []FieldError

func (e ValidationError) Error() string {
	if len(e) == 1 {
		return e[0].Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d problems:", len(e))
	for _, fe := range e {
		b.WriteString("\n  - ")
		b.WriteString(fe.Error())
	}
	return b.String()
}

const (
	// NamePattern matches names of manifests, targets, distros and
	// repositories: lowercase alphanumerics, '-' and '.', e.g. "ubuntu-24.04".
	NamePattern = `^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`
	// PackageNamePattern matches Debian and RPM package names, e.g.
	// "libstdc++6" or "NetworkManager".
	PackageNamePattern = `^[A-Za-z0-9][A-Za-z0-9+._-]*$`
)

var (
	nameRE        = regexp.MustCompile(NamePattern)
	packageNameRE = regexp.MustCompile(PackageNamePattern)
)

const maxNameLength = 63

// ValidateOfflineRepository checks the manifest and returns a ValidationError
// listing every problem, or nil. It expects the defaults of the manifest's API
// version to be applied, as they are when decoding. Checks that need the
// built-in catalog or the network, such as whether a distro exists, happen
// later.
func ValidateOfflineRepository(r *packtrix.OfflineRepository) error {
	v := &validator{}
	v.name("metadata.name", r.Name)

	s := &r.Spec
	targets := v.targets("spec.targets", s.Targets)
	v.packages("spec.packages", s.Packages, targets)
	v.autoRepos("spec.autoRepos", s.AutoRepos)
	v.resolve("spec.resolve", s.Resolve)
	v.updates("spec.updates", s.Updates)
	v.publish("spec.publish", s.Publish)
	v.export("spec.export", s.Export)

	if len(v.errs) == 0 {
		return nil
	}
	return v.errs
}

type validator struct {
	errs ValidationError
}

func (v *validator) add(field, format string, args ...any) {
	v.errs = append(v.errs, FieldError{Field: field, Detail: fmt.Sprintf(format, args...)})
}

func (v *validator) name(field, name string) {
	switch {
	case name == "":
		v.add(field, "required")
	case len(name) > maxNameLength:
		v.add(field, "must be at most %d characters", maxNameLength)
	case !nameRE.MatchString(name):
		v.add(field, "invalid name %q: use lowercase letters, digits, '-' and '.'", name)
	}
}

// targets validates the targets and returns their names.
func (v *validator) targets(field string, targets []packtrix.Target) []string {
	if len(targets) == 0 {
		v.add(field, "at least one target is required")
	}
	var names []string
	for i, t := range targets {
		f := fmt.Sprintf("%s[%d]", field, i)
		switch {
		case t.Distro != nil && len(t.Repos) > 0:
			v.add(f, "set either distro or repos, not both")
		case t.Distro == nil && len(t.Repos) == 0:
			v.add(f, "set distro (a built-in distribution) or repos (a distribution defined from scratch)")
		}
		if t.Distro != nil {
			v.distro(f+".distro", *t.Distro)
		}
		if t.Distro == nil && len(t.ExtraRepos) > 0 {
			v.add(f+".extraRepos", "only allowed with distro; for a distribution defined from scratch, list every repository under repos")
		}

		v.name(f+".name", t.Name)
		switch {
		case t.Name == packtrix.SelectorDeb || t.Name == packtrix.SelectorRPM:
			v.add(f+".name", "%q is reserved: it selects every %s target in only/except", t.Name, t.Name)
		case slices.Contains(names, t.Name):
			v.add(f+".name", "duplicate target name %q", t.Name)
		case t.Name != "":
			names = append(names, t.Name)
		}

		for j, arch := range t.Architectures {
			af := fmt.Sprintf("%s.architectures[%d]", f, j)
			switch {
			case !slices.Contains(packtrix.SupportedArchitectures, arch):
				v.add(af, "unsupported architecture %q (supported: %s)", arch, strings.Join(packtrix.SupportedArchitectures, ", "))
			case slices.Contains(t.Architectures[:j], arch):
				v.add(af, "duplicate architecture %q", arch)
			}
		}

		v.repositories(f+".extraRepos", t.ExtraRepos, false)
		v.repositories(f+".repos", t.Repos, true)
	}
	return names
}

func (v *validator) distro(field string, d packtrix.Distro) {
	v.name(field+".name", d.Name)
	v.list(field+".components", d.Components, nameRE, "component")
	if d.Mirror != "" {
		v.url(field+".mirror", d.Mirror)
	}
	if d.Auth != nil {
		v.auth(field+".auth", *d.Auth)
	}
}

// repositories validates a repository list. With sameType, every repository
// must have the same type, as a target defined from scratch can't mix formats.
func (v *validator) repositories(field string, repos []packtrix.Repository, sameType bool) {
	var names []string
	var firstType packtrix.RepoType
	for i, r := range repos {
		f := fmt.Sprintf("%s[%d]", field, i)
		v.name(f+".name", r.Name)
		if r.Name != "" && slices.Contains(names, r.Name) {
			v.add(f+".name", "duplicate repository name %q", r.Name)
		}
		names = append(names, r.Name)

		switch r.Type {
		case packtrix.RepoTypeAPT:
			flat := strings.HasSuffix(r.Suite, "/")
			switch {
			case r.Suite == "":
				v.add(f+".suite", `required for apt repositories, e.g. "stable", or "/" for a flat repository`)
			case flat && len(r.Components) > 0:
				v.add(f+".components", `must be empty for a flat repository (suite ending in "/")`)
			case !flat && len(r.Components) == 0:
				v.add(f+".components", "required for apt repositories, e.g. [main]")
			}
			v.list(f+".components", r.Components, nameRE, "component")
		case packtrix.RepoTypeRPM:
			if r.Suite != "" {
				v.add(f+".suite", "only used by apt repositories")
			}
			if len(r.Components) > 0 {
				v.add(f+".components", "only used by apt repositories")
			}
		case "":
			v.add(f+".type", "required: apt or rpm")
		default:
			v.add(f+".type", "must be apt or rpm, got %q", r.Type)
		}
		if sameType && (r.Type == packtrix.RepoTypeAPT || r.Type == packtrix.RepoTypeRPM) {
			if firstType == "" {
				firstType = r.Type
			} else if r.Type != firstType {
				v.add(f+".type", "all repositories of a target must have the same type, found %s and %s", firstType, r.Type)
			}
		}

		v.url(f+".url", r.URL)
		if r.GPGKey == "" {
			v.add(f+".gpgKey", "required: path to the repository's public signing key")
		}
		if r.Auth != nil {
			v.auth(f+".auth", *r.Auth)
		}
	}
}

func (v *validator) url(field, raw string) {
	if raw == "" {
		v.add(field, "required")
		return
	}
	expanded := strings.ReplaceAll(raw, "{arch}", "x86_64")
	if strings.ContainsAny(expanded, "{}") {
		v.add(field, "unknown placeholder in %q: only {arch} is supported", raw)
		return
	}
	u, err := url.Parse(expanded)
	switch {
	case err != nil:
		v.add(field, "invalid URL %q", raw)
	case u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "file":
		v.add(field, "must be an http, https or file URL, got %q", raw)
	case u.Scheme != "file" && u.Host == "":
		v.add(field, "missing host in %q", raw)
	}
}

func (v *validator) auth(field string, a packtrix.Auth) {
	if a == (packtrix.Auth{}) {
		v.add(field, "set at least one credential")
		return
	}
	if (a.Username == "") != (a.Password == "") {
		v.add(field, "username and password must be set together")
	}
	if a.Password != "" && a.Token != "" {
		v.add(field, "set either password or token, not both")
	}
	if (a.ClientCert == "") != (a.ClientKey == "") {
		v.add(field, "clientCert and clientKey must be set together")
	}
	v.secret(field+".password", a.Password)
	v.secret(field+".token", a.Token)
}

func (v *validator) secret(field string, ref packtrix.SecretRef) {
	if ref == "" {
		return
	}
	if err := ref.Validate(); err != nil {
		v.add(field, "%v", err)
	}
}

func (v *validator) packages(field string, pkgs []packtrix.Package, targets []string) {
	if len(pkgs) == 0 {
		v.add(field, "at least one package is required")
	}
	firstIndex := map[string]int{}
	for i, p := range pkgs {
		f := fmt.Sprintf("%s[%d]", field, i)
		switch {
		case p.Name == "":
			v.add(f+".name", "required")
		case !packageNameRE.MatchString(p.Name):
			v.add(f+".name", "invalid package name %q", p.Name)
		}
		if j, dup := firstIndex[p.Name]; dup && p.Name != "" {
			v.add(f+".name", "duplicate package %q (also at %s[%d])", p.Name, field, j)
		} else {
			firstIndex[p.Name] = i
		}
		if p.Deb != "" && !packageNameRE.MatchString(p.Deb) {
			v.add(f+".deb", "invalid package name %q", p.Deb)
		}
		if p.RPM != "" && !packageNameRE.MatchString(p.RPM) {
			v.add(f+".rpm", "invalid package name %q", p.RPM)
		}
		if strings.ContainsAny(string(p.Version), " \t") {
			v.add(f+".version", "must not contain spaces, got %q", p.Version)
		}
		if len(p.Only) > 0 && len(p.Except) > 0 {
			v.add(f, "set either only or except, not both")
		}
		v.selectors(f+".only", p.Only, targets)
		v.selectors(f+".except", p.Except, targets)
		if p.Repo != "" && !nameRE.MatchString(p.Repo) {
			v.add(f+".repo", "invalid repository name %q", p.Repo)
		}
	}
}

func (v *validator) selectors(field string, selectors, targets []string) {
	for i, s := range selectors {
		if s == packtrix.SelectorDeb || s == packtrix.SelectorRPM || slices.Contains(targets, s) {
			continue
		}
		v.add(fmt.Sprintf("%s[%d]", field, i), "unknown target %q (targets: %s; or deb, rpm)", s, strings.Join(targets, ", "))
	}
}

func (v *validator) autoRepos(field string, a packtrix.AutoRepos) {
	for i, pattern := range a.Deny {
		if _, err := path.Match(pattern, ""); err != nil || pattern == "" {
			v.add(fmt.Sprintf("%s.deny[%d]", field, i), "invalid pattern %q", pattern)
		}
	}
}

func (v *validator) resolve(field string, r packtrix.Resolve) {
	switch {
	case r.Baseline == packtrix.BaselineMinimal, r.Baseline == packtrix.BaselineNone:
	case strings.HasPrefix(r.Baseline, packtrix.BaselineLockPrefix) && len(r.Baseline) > len(packtrix.BaselineLockPrefix):
	default:
		v.add(field+".baseline", `must be "minimal", "none" or "lock:<path>", got %q`, r.Baseline)
	}
	v.oneOf(field+".alternatives", r.Alternatives, packtrix.AlternativesFirst, packtrix.AlternativesAll)
	v.list(field+".exclude", r.Exclude, packageNameRE, "package name")
}

func (v *validator) updates(field string, u packtrix.Updates) {
	if u.Check != "" && len(strings.Fields(u.Check)) != 5 {
		v.add(field+".check", `must be a cron expression with 5 fields, e.g. "0 2 * * *", got %q`, u.Check)
	}
	v.oneOf(field+".accept", u.Accept, packtrix.AcceptAll, packtrix.AcceptSecurity, packtrix.AcceptNone)
	if u.MinAge < 0 {
		v.add(field+".minAge", "must not be negative")
	}
	v.oneOf(field+".publish", u.Publish, packtrix.PublishApproval, packtrix.PublishAuto)
}

func (v *validator) publish(field string, p packtrix.Publish) {
	if p.Path == "" {
		v.add(field+".path", "required")
	}
	if p.KeepSnapshots != nil && *p.KeepSnapshots < 0 {
		v.add(field+".keepSnapshots", "must be 0 or more")
	}
	v.secret(field+".signing.key", p.Signing.Key)
}

func (v *validator) export(field string, e packtrix.Export) {
	v.oneOf(field+".format", e.Format, packtrix.ExportFormatTarZst)
	if e.SplitSize != 0 && e.SplitSize < packtrix.MiB {
		v.add(field+".splitSize", "must be at least 1MiB, got %s", e.SplitSize)
	}
}

func (v *validator) oneOf(field, value string, allowed ...string) {
	if !slices.Contains(allowed, value) {
		v.add(field, "must be one of %s, got %q", strings.Join(allowed, ", "), value)
	}
}

// list checks that every item matches re and appears once.
func (v *validator) list(field string, items []string, re *regexp.Regexp, what string) {
	for i, item := range items {
		f := fmt.Sprintf("%s[%d]", field, i)
		switch {
		case !re.MatchString(item):
			v.add(f, "invalid %s %q", what, item)
		case slices.Contains(items[:i], item):
			v.add(f, "duplicate %s %q", what, item)
		}
	}
}
