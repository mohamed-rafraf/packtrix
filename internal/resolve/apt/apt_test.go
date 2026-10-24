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
	"reflect"
	"strings"
	"testing"

	"github.com/packtrix/packtrix/internal/deb"
	"github.com/packtrix/packtrix/internal/resolve"
)

// parse reads packages from a Packages file. A paragraph without an
// Architecture is amd64.
func parse(t *testing.T, text string) []*deb.Package {
	t.Helper()
	paragraphs := strings.Split(strings.TrimSpace(text), "\n\n")
	for i, p := range paragraphs {
		if !strings.Contains(p, "Architecture:") {
			paragraphs[i] = p + "\nArchitecture: amd64"
		}
	}
	var packages []*deb.Package
	err := deb.ReadParagraphs(strings.NewReader(strings.Join(paragraphs, "\n\n")), func(p deb.Paragraph) error {
		pkg, err := deb.ParsePackage(p)
		packages = append(packages, pkg)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return packages
}

func index(t *testing.T, text string) *Index {
	t.Helper()
	return NewIndex("amd64", parse(t, text))
}

// pick selects "name" or "name=constraint" from the index.
func pick(t *testing.T, ix *Index, s string) *deb.Package {
	t.Helper()
	name, version, _ := strings.Cut(s, "=")
	c, err := deb.ParseConstraint(version)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ix.Select(name, c)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type run struct {
	opts     Options
	baseline []string
	listed   []string
}

func (r run) closure(t *testing.T, ix *Index) ([]string, error) {
	t.Helper()
	var baseline, listed []*deb.Package
	for _, s := range r.baseline {
		baseline = append(baseline, pick(t, ix, s))
	}
	for _, s := range r.listed {
		listed = append(listed, pick(t, ix, s))
	}
	result, err := resolve.Closure(NewModel(ix, baseline, r.opts), listed)
	if err != nil {
		return nil, err
	}
	var got []string
	for _, p := range result.Packages {
		got = append(got, p.String())
	}
	return got, nil
}

func check(t *testing.T, ix *Index, runs map[string]run, want map[string][]string) {
	t.Helper()
	for name, r := range runs {
		got, err := r.closure(t, ix)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !reflect.DeepEqual(got, want[name]) {
			t.Errorf("%s: closure = %q, want %q", name, got, want[name])
		}
	}
}

func TestDepends(t *testing.T) {
	ix := index(t, `
Package: nginx
Version: 1.24.0-2ubuntu7
Pre-Depends: dpkg (>= 1.15)
Depends: libc6 (>= 2.34), libssl3t64 (>= 3.0.0)

Package: dpkg
Version: 1.22.6ubuntu6

Package: libc6
Version: 2.39-0ubuntu8

Package: libc6
Version: 2.39-0ubuntu8.4

Package: libssl3t64
Version: 3.0.13-0ubuntu3.4
Depends: libc6 (>= 2.39-0ubuntu8.4)

Package: libssl3t64
Version: 2.9
`)
	check(t, ix, map[string]run{
		"newest versions":        {listed: []string{"nginx"}},
		"baseline":               {listed: []string{"nginx"}, baseline: []string{"dpkg", "libc6=2.39-0ubuntu8"}},
		"listed in the baseline": {listed: []string{"nginx", "dpkg"}, baseline: []string{"dpkg", "libc6=2.39-0ubuntu8.4"}},
	}, map[string][]string{
		"newest versions": {"nginx=1.24.0-2ubuntu7", "dpkg=1.22.6ubuntu6", "libc6=2.39-0ubuntu8.4", "libssl3t64=3.0.13-0ubuntu3.4"},
		// The baseline's libc6 is enough for nginx, not for libssl3t64.
		"baseline":               {"nginx=1.24.0-2ubuntu7", "libssl3t64=3.0.13-0ubuntu3.4", "libc6=2.39-0ubuntu8.4"},
		"listed in the baseline": {"nginx=1.24.0-2ubuntu7", "dpkg=1.22.6ubuntu6", "libssl3t64=3.0.13-0ubuntu3.4"},
	})
}

func TestRecommends(t *testing.T) {
	ix := index(t, `
Package: curl
Version: 8.5.0
Recommends: ca-certificates, curl-docs

Package: ca-certificates
Version: 20240203
`)
	check(t, ix, map[string]run{
		"off": {listed: []string{"curl"}},
		"on":  {listed: []string{"curl"}, opts: Options{Recommends: true}},
	}, map[string][]string{
		"off": {"curl=8.5.0"},
		// apt ignores curl-docs, which no package provides.
		"on": {"curl=8.5.0", "ca-certificates=20240203"},
	})
}

func TestAlternatives(t *testing.T) {
	ix := index(t, `
Package: app
Version: 1.0
Depends: libfoo-missing | libbar, exim4 | postfix

Package: libbar
Version: 1.0

Package: exim4
Version: 4.97

Package: postfix
Version: 3.8.6
`)
	check(t, ix, map[string]run{
		"first":                {listed: []string{"app"}},
		"all":                  {listed: []string{"app"}, opts: Options{AllAlternatives: true}},
		"satisfied by another": {listed: []string{"app"}, baseline: []string{"postfix"}},
	}, map[string][]string{
		"first":                {"app=1.0", "libbar=1.0", "exim4=4.97"},
		"all":                  {"app=1.0", "libbar=1.0", "exim4=4.97", "postfix=3.8.6"},
		"satisfied by another": {"app=1.0", "libbar=1.0"},
	})
}

func TestVirtualPackages(t *testing.T) {
	ix := index(t, `
Package: app
Version: 1.0
Depends: awk, editor, libcurl4 (>= 8.0)

Package: mawk
Version: 1.3.4
Provides: awk

Package: gawk
Version: 5.2.1
Provides: awk

Package: editor
Version: 1.0

Package: nano
Version: 7.2
Provides: editor

Package: libcurl4-old
Version: 7.0
Provides: libcurl4

Package: libcurl4t64
Version: 8.5.0
Provides: libcurl4 (= 8.5.0)

Package: app-old
Version: 1.0
Depends: awk (>= 1.0)
`)
	check(t, ix, map[string]run{
		"providers": {listed: []string{"app"}},
		"baseline":  {listed: []string{"app"}, baseline: []string{"mawk"}},
	}, map[string][]string{
		// The first provider by name; the real editor before nano; only a
		// versioned Provides satisfies a versioned dependency.
		"providers": {"app=1.0", "gawk=5.2.1", "editor=1.0", "libcurl4t64=8.5.0"},
		"baseline":  {"app=1.0", "editor=1.0", "libcurl4t64=8.5.0"},
	})

	// No Provides of awk is versioned.
	_, err := run{listed: []string{"app-old"}}.closure(t, ix)
	if want := "app-old=1.0 needs awk (>= 1.0), which no package provides"; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func TestArchitectures(t *testing.T) {
	ix := index(t, `
Package: app
Version: 1.0
Depends: python3:any, libc6:amd64, libc6:i386 | lib32-compat, tzdata

Package: python3
Version: 3.12.3
Multi-Arch: allowed

Package: libc6
Version: 2.39
Multi-Arch: same

Package: libc6
Version: 2.40
Architecture: i386

Package: lib32-compat
Version: 1.0

Package: tzdata
Version: 2024a
Architecture: all

Package: arm-tool
Version: 1.0
Architecture: arm64

Package: needs-arm-tool
Version: 1.0
Depends: arm-tool
`)
	check(t, ix, map[string]run{
		"qualifiers": {listed: []string{"app"}},
	}, map[string][]string{
		// libc6 2.40 is i386, so it isn't in the index.
		"qualifiers": {"app=1.0", "python3=3.12.3", "libc6=2.39", "lib32-compat=1.0", "tzdata=2024a"},
	})

	_, err := run{listed: []string{"needs-arm-tool"}}.closure(t, ix)
	if want := "needs-arm-tool=1.0 needs arm-tool, which no package provides"; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func TestBreaksAndConflicts(t *testing.T) {
	ix := index(t, `
Package: app
Version: 2.0
Breaks: libfoo (<< 2.0), libbar (<< 3), libnew (<< 0.5)
Conflicts: libbaz, libqux (<< 1.0), libzed:i386 (<< 2.0)

Package: libfoo
Version: 1.5

Package: libfoo
Version: 2.1
Depends: libfoo-data

Package: libfoo-data
Version: 2.1

Package: libbar
Version: 2.0

Package: libbaz
Version: 1.0

Package: libqux
Version: 0.9

Package: libqux
Version: 1.2

Package: libnew
Version: 1.0

Package: libnew
Version: 2.0

Package: libzed
Version: 1.0

Package: libzed
Version: 2.0
`)
	check(t, ix, map[string]run{
		"empty baseline":     {listed: []string{"app"}},
		"old libfoo":         {listed: []string{"app"}, baseline: []string{"libfoo=1.5"}},
		"new libfoo":         {listed: []string{"app"}, baseline: []string{"libfoo=2.1"}},
		"nothing to upgrade": {listed: []string{"app"}, baseline: []string{"libbar", "libbaz", "libzed=1.0"}},
		"old libqux":         {listed: []string{"app"}, baseline: []string{"libqux=0.9"}},
	}, map[string][]string{
		// No version of libnew is broken, so there is nothing to upgrade.
		"empty baseline": {"app=2.0"},
		// apt upgrades libfoo, so the repository needs 2.1 and its
		// dependencies.
		"old libfoo": {"app=2.0", "libfoo=2.1", "libfoo-data=2.1"},
		"new libfoo": {"app=2.0"},
		// No version of libbar avoids the Breaks, and none of libbaz the
		// unversioned Conflicts: apt removes them. The Conflicts on libzed
		// is about i386.
		"nothing to upgrade": {"app=2.0"},
		"old libqux":         {"app=2.0", "libqux=1.2"},
	})
}

func TestWhy(t *testing.T) {
	ix := index(t, `
Package: nginx
Version: 1.24.0
Depends: libssl3t64 (>= 3.0), libpcre2-8-0

Package: libssl3t64
Version: 3.0.13
Depends: libbrotli1 (>= 2.0)

Package: libbrotli1
Version: 1.1.0
`)
	result, err := resolve.Closure(NewModel(ix, nil, Options{}), []*deb.Package{pick(t, ix, "libbrotli1")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Why(pick(t, ix, "libbrotli1")), "libbrotli1=1.1.0 ← listed"; got != want {
		t.Errorf("Why = %q, want %q", got, want)
	}

	_, err = run{listed: []string{"nginx"}}.closure(t, ix)
	want := `2 unresolved dependencies:
  - nginx=1.24.0 needs libpcre2-8-0, which no package provides
  - nginx=1.24.0 → libssl3t64=3.0.13 needs libbrotli1 (>= 2.0), which no package provides`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func TestSelect(t *testing.T) {
	ix := index(t, `
Package: kubelet
Version: 1.34.0-1.1

Package: kubelet
Version: 1.35.0-1.1

Package: kubelet
Version: 1.34.1-1.1

Package: postfix
Version: 3.8.6
Provides: mail-transport-agent

Package: exim4-daemon-light
Version: 4.97
Provides: mail-transport-agent

Package: exim4-daemon-heavy
Version: 4.97
Provides: mail-transport-agent

Package: nano
Version: 7.2
Provides: editor
`)
	for _, tt := range []struct{ name, constraint, want string }{
		{"kubelet", "", "kubelet=1.35.0-1.1"},
		{"kubelet", "1.34.*", "kubelet=1.34.1-1.1"},
		{"kubelet", "1.34.0", "kubelet=1.34.0-1.1"},
		{"editor", "", "nano=7.2"},
		{"kubelet", "1.33.*", "no version of kubelet matches 1.33.*; the repositories have 1.35.0-1.1, 1.34.1-1.1, 1.34.0-1.1"},
		{"kubeadm", "", "no package kubeadm in the target's repositories"},
		{"mail-transport-agent", "", "mail-transport-agent is a virtual package; list one of the packages that provide it: exim4-daemon-heavy, exim4-daemon-light, postfix"},
	} {
		c, err := deb.ParseConstraint(tt.constraint)
		if err != nil {
			t.Fatal(err)
		}
		var got string
		if p, err := ix.Select(tt.name, c); err != nil {
			got = err.Error()
		} else {
			got = p.String()
		}
		if got != tt.want {
			t.Errorf("Select(%s, %q) = %q, want %q", tt.name, tt.constraint, got, tt.want)
		}
	}
}

func TestIndexKeepsFirstRepository(t *testing.T) {
	updates := parse(t, "Package: curl\nVersion: 8.5.0-2ubuntu10.6\n")
	security := parse(t, "Package: curl\nVersion: 8.5.0-2ubuntu10.6\n")
	release := parse(t, "Package: curl\nVersion: 8.5.0-2ubuntu10\n")
	ix := NewIndex("amd64", append(append(release, updates...), security...))
	if got := ix.packages["curl"]; len(got) != 2 || got[0] != updates[0] || got[1] != release[0] {
		t.Errorf("curl versions = %v, want the updates one, then the release one", got)
	}
}
