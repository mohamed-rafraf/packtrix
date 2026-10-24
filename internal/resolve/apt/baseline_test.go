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
	"testing"

	"github.com/packtrix/packtrix/internal/deb"
)

func names(packages []*deb.Package) []string {
	var out []string
	for _, p := range packages {
		out = append(out, p.String())
	}
	return out
}

func TestMinimalBaseline(t *testing.T) {
	release := parse(t, `
Package: dpkg
Version: 1.22.6
Essential: yes
Priority: required
Pre-Depends: libc6 (>= 2.34)

Package: libc6
Version: 2.39-0ubuntu8
Priority: required

Package: libc6
Version: 2.40
Priority: required
Architecture: i386

Package: apt
Version: 2.7.14
Priority: important
Depends: adduser | passwd, libapt-pkg6.0t64 (>= 2.7.14)

Package: adduser
Version: 3.137
Priority: important
Depends: passwd

Package: passwd
Version: 4.13
Priority: optional

Package: libapt-pkg6.0t64
Version: 2.7.14
Priority: optional

Package: diffutils
Version: 3.10
Essential: yes
Priority: optional

Package: curl
Version: 8.5.0
Priority: optional
`)
	baseline, err := Baseline("minimal", "amd64", release)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"dpkg=1.22.6", "libc6=2.39-0ubuntu8", "apt=2.7.14", "adduser=3.137", "diffutils=3.10", "libapt-pkg6.0t64=2.7.14", "passwd=4.13"}
	if got := names(baseline); !reflect.DeepEqual(got, want) {
		t.Errorf("baseline = %q, want %q", got, want)
	}
}

func TestMinimalBaselineUnresolved(t *testing.T) {
	release := parse(t, `
Package: dpkg
Version: 1.22.6
Essential: yes
Pre-Depends: libc6 (>= 2.34)
`)
	_, err := MinimalBaseline("amd64", release)
	if want := "minimal baseline: dpkg=1.22.6 needs libc6 (>= 2.34), which no package provides"; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func TestBaselineSettings(t *testing.T) {
	release := parse(t, "Package: dpkg\nVersion: 1.22.6\nEssential: yes\n")
	for _, tt := range []struct{ setting, want string }{
		{"none", ""},
		{"lock:host.lock", `baseline "lock:host.lock": baselines from a lockfile aren't supported yet; they come with host discovery`},
		{"full", `unknown baseline "full"`},
	} {
		baseline, err := Baseline(tt.setting, "amd64", release)
		var got string
		if err != nil {
			got = err.Error()
		}
		if got != tt.want || len(baseline) != 0 {
			t.Errorf("Baseline(%q) = %q, %v, want no packages and %q", tt.setting, names(baseline), err, tt.want)
		}
	}
}

// A package listed with baseline none brings the C library in; with the
// minimal baseline, only what the release doesn't have.
func TestBaselineChangesTheClosure(t *testing.T) {
	release := parse(t, `
Package: libc6
Version: 2.39-0ubuntu8
Priority: required

Package: curl
Version: 8.5.0-2ubuntu10
Priority: optional
Depends: libc6 (>= 2.34)
`)
	updates := parse(t, `
Package: libc6
Version: 2.39-0ubuntu8.4
Priority: required

Package: curl
Version: 8.5.0-2ubuntu10.6
Priority: optional
Depends: libc6 (>= 2.34), libcurl4t64 (= 8.5.0-2ubuntu10.6)

Package: libcurl4t64
Version: 8.5.0-2ubuntu10.6
Depends: libc6 (>= 2.39-0ubuntu8.4)
`)
	ix := NewIndex("amd64", append(updates, release...))
	for _, tt := range []struct {
		setting string
		want    []string
	}{
		{"none", []string{"curl=8.5.0-2ubuntu10.6", "libc6=2.39-0ubuntu8.4", "libcurl4t64=8.5.0-2ubuntu10.6"}},
		// The release's libc6 is enough for curl, not for libcurl4t64.
		{"minimal", []string{"curl=8.5.0-2ubuntu10.6", "libcurl4t64=8.5.0-2ubuntu10.6", "libc6=2.39-0ubuntu8.4"}},
	} {
		baseline, err := Baseline(tt.setting, "amd64", release)
		if err != nil {
			t.Fatal(err)
		}
		r := run{listed: []string{"curl"}}
		for _, p := range baseline {
			r.baseline = append(r.baseline, p.String())
		}
		got, err := r.closure(t, ix)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("baseline %s: closure = %q, want %q", tt.setting, got, tt.want)
		}
	}
}

// The baseline comes from the release's index, the closure from the
// target's: they match by version, not by object.
func TestBaselineMatchesByVersion(t *testing.T) {
	release := parse(t, "Package: libc6\nVersion: 2.39-0ubuntu8\n")
	target := parse(t, "Package: libc6\nVersion: 2.39-0ubuntu8\n\nPackage: libc6\nVersion: 2.39-0ubuntu8.4\n")
	m := NewModel(NewIndex("amd64", target), release, Options{})
	if !m.InBaseline(target[0]) || m.InBaseline(target[1]) {
		t.Errorf("InBaseline = %t, %t, want only the release version", m.InBaseline(target[0]), m.InBaseline(target[1]))
	}
}
