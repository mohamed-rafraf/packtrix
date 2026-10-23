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

package resolve

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type pkg string

func (p pkg) String() string { return string(p) }

// fixture is a model written as a table: each package's needs, and the
// baseline.
type fixture struct {
	needs    map[pkg][]Need[pkg]
	baseline []pkg
	errs     map[pkg]error
	calls    map[pkg]int
}

func (f *fixture) Needs(p pkg) ([]Need[pkg], error) {
	if f.calls == nil {
		f.calls = map[pkg]int{}
	}
	f.calls[p]++
	return f.needs[p], f.errs[p]
}

func (f *fixture) InBaseline(p pkg) bool {
	for _, b := range f.baseline {
		if b == p {
			return true
		}
	}
	return false
}

// need is a need satisfied by any of the groups.
func need(text string, groups ...[]pkg) Need[pkg] {
	return Need[pkg]{Text: text, Alternatives: groups}
}

func group(ps ...pkg) []pkg { return ps }

func mustClosure(t *testing.T, f *fixture, listed ...pkg) *Result[pkg] {
	t.Helper()
	r, err := Closure[pkg](f, listed)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestChainAndBaseline(t *testing.T) {
	f := &fixture{
		needs: map[pkg][]Need[pkg]{
			"nginx":   {need("libssl3 (>= 3.0)", group("libssl3")), need("libc6", group("libc6"))},
			"libssl3": {need("libc6 (>= 2.34)", group("libc6"))},
		},
		baseline: []pkg{"libc6"},
	}
	r := mustClosure(t, f, "nginx")
	if want := []pkg{"nginx", "libssl3"}; !reflect.DeepEqual(r.Packages, want) {
		t.Errorf("closure = %v, want %v (libc6 is in the baseline)", r.Packages, want)
	}
	if got, want := r.Why("libssl3"), "libssl3 ← nginx ← listed"; got != want {
		t.Errorf("Why(libssl3) = %q, want %q", got, want)
	}
	if got, want := r.Why("nginx"), "nginx ← listed"; got != want {
		t.Errorf("Why(nginx) = %q, want %q", got, want)
	}
	if got := r.Why("libc6"); got != "" {
		t.Errorf("Why(libc6) = %q, want nothing for a package outside the closure", got)
	}
	if reason, _ := r.Reason("libssl3"); reason.Need != "libssl3 (>= 3.0)" || *reason.By != "nginx" {
		t.Errorf("reason = %+v, want nginx and the need that added libssl3", reason)
	}
}

func TestListedPackagesAreAlwaysIncluded(t *testing.T) {
	f := &fixture{baseline: []pkg{"curl"}}
	if r := mustClosure(t, f, "curl", "curl"); !reflect.DeepEqual(r.Packages, []pkg{"curl"}) {
		t.Errorf("closure = %v, want curl once, although the baseline has it", r.Packages)
	}
}

func TestCycle(t *testing.T) {
	f := &fixture{needs: map[pkg][]Need[pkg]{
		"aa": {need("bb", group("bb"))},
		"bb": {need("aa", group("aa"))},
	}}
	r := mustClosure(t, f, "aa")
	if want := []pkg{"aa", "bb"}; !reflect.DeepEqual(r.Packages, want) {
		t.Errorf("closure = %v, want %v", r.Packages, want)
	}
	for p, n := range f.calls {
		if n != 1 {
			t.Errorf("Needs(%s) called %d times, want once", p, n)
		}
	}
}

func TestAlternatives(t *testing.T) {
	mta := need("default-mta | mail-transport-agent", group("postfix"), group(), group("exim4", "sendmail"))
	tests := []struct {
		name     string
		need     Need[pkg]
		baseline []pkg
		want     []pkg
	}{
		{"first non-empty group", mta, nil, []pkg{"mutt", "postfix"}},
		{"every group", Need[pkg]{Text: mta.Text, Alternatives: mta.Alternatives, All: true}, nil, []pkg{"mutt", "postfix", "exim4"}},
		{"satisfied by a later group", mta, []pkg{"sendmail"}, []pkg{"mutt"}},
		{"satisfied by the first group", mta, []pkg{"postfix"}, []pkg{"mutt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fixture{needs: map[pkg][]Need[pkg]{"mutt": {tt.need}}, baseline: tt.baseline}
			if r := mustClosure(t, f, "mutt"); !reflect.DeepEqual(r.Packages, tt.want) {
				t.Errorf("closure = %v, want %v", r.Packages, tt.want)
			}
		})
	}
}

func TestSatisfiedByPackageAlreadyInClosure(t *testing.T) {
	f := &fixture{needs: map[pkg][]Need[pkg]{
		"app": {need("mail-transport-agent", group("postfix"), group("exim4"))},
	}}
	if r := mustClosure(t, f, "exim4", "app"); !reflect.DeepEqual(r.Packages, []pkg{"exim4", "app"}) {
		t.Errorf("closure = %v, want exim4 to satisfy app", r.Packages)
	}
}

// An RPM "(langpacks-de if glibc)": the need only applies once its condition
// holds, even when that happens after the need was first seen.
func TestCondition(t *testing.T) {
	conditional := Need[pkg]{Text: "(langpacks-de if glibc)", Alternatives: [][]pkg{group("langpacks-de")}, Condition: []pkg{"glibc"}}
	needs := map[pkg][]Need[pkg]{
		"tool":  {conditional},
		"other": {need("helper", group("helper"))},
		// helper brings glibc in, which turns tool's need on.
		"helper": {need("glibc", group("glibc"))},
	}
	if r := mustClosure(t, &fixture{needs: needs}, "tool"); !reflect.DeepEqual(r.Packages, []pkg{"tool"}) {
		t.Errorf("without glibc: closure = %v, want only tool", r.Packages)
	}
	r := mustClosure(t, &fixture{needs: needs}, "tool", "other")
	if want := []pkg{"tool", "other", "helper", "glibc", "langpacks-de"}; !reflect.DeepEqual(r.Packages, want) {
		t.Errorf("with glibc: closure = %v, want %v", r.Packages, want)
	}
}

// A package that breaks the baseline's libfoo1 makes the machines upgrade it,
// so the closure must contain the newer version.
func TestConditionOnBaselineForcesUpgrade(t *testing.T) {
	upgrade := Need[pkg]{Text: "Breaks: libfoo (<< 2.0)", Alternatives: [][]pkg{group("libfoo-2.1")}, Condition: []pkg{"libfoo-1.5"}}
	needs := map[pkg][]Need[pkg]{"newapp": {upgrade}}
	r := mustClosure(t, &fixture{needs: needs, baseline: []pkg{"libfoo-1.5"}}, "newapp")
	if want := []pkg{"newapp", "libfoo-2.1"}; !reflect.DeepEqual(r.Packages, want) {
		t.Errorf("closure = %v, want %v", r.Packages, want)
	}
	if r := mustClosure(t, &fixture{needs: needs}, "newapp"); !reflect.DeepEqual(r.Packages, []pkg{"newapp"}) {
		t.Errorf("without libfoo-1.5: closure = %v, want only newapp", r.Packages)
	}
}

func TestUnresolved(t *testing.T) {
	f := &fixture{needs: map[pkg][]Need[pkg]{
		"nginx": {need("libfoo (>= 2)")},
		"curl":  {need("libcurl4", group("libcurl4"))},
		"libcurl4": {
			need("libbar", group(), group()),
			need("libc6", group("libc6")),
		},
	}}
	r, err := Closure[pkg](f, []pkg{"nginx", "curl"})
	if r != nil {
		t.Errorf("result = %v, want none when something is unresolved", r.Packages)
	}
	var unresolved *UnresolvedError[pkg]
	if !errors.As(err, &unresolved) {
		t.Fatalf("error = %v, want an *UnresolvedError", err)
	}
	want := []Failure[pkg]{
		{Path: []pkg{"nginx"}, Need: "libfoo (>= 2)"},
		{Path: []pkg{"curl", "libcurl4"}, Need: "libbar"},
	}
	if !reflect.DeepEqual(unresolved.Failures, want) {
		t.Errorf("failures = %+v, want %+v", unresolved.Failures, want)
	}
	for _, msg := range []string{
		"2 unresolved dependencies",
		"nginx needs libfoo (>= 2), which no package provides",
		"curl → libcurl4 needs libbar, which no package provides",
	} {
		if !strings.Contains(err.Error(), msg) {
			t.Errorf("error %q doesn't contain %q", err, msg)
		}
	}
}

func TestUnresolvedOne(t *testing.T) {
	f := &fixture{needs: map[pkg][]Need[pkg]{"app": {need("libfoo")}}}
	_, err := Closure[pkg](f, []pkg{"app"})
	if want := "app needs libfoo, which no package provides"; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func TestDeterministic(t *testing.T) {
	needs := map[pkg][]Need[pkg]{
		"aa": {need("cc", group("cc")), need("bb", group("bb"))},
		"bb": {need("ee", group("ee")), need("dd", group("dd"))},
		"cc": {need("dd", group("dd")), need("ff", group("ff"))},
	}
	want := []pkg{"aa", "cc", "bb", "dd", "ff", "ee"}
	for range 20 {
		if r := mustClosure(t, &fixture{needs: needs}, "aa"); !reflect.DeepEqual(r.Packages, want) {
			t.Fatalf("closure = %v, want %v every time", r.Packages, want)
		}
	}
}

func TestModelError(t *testing.T) {
	f := &fixture{
		needs: map[pkg][]Need[pkg]{"app": {need("lib", group("lib"))}},
		errs:  map[pkg]error{"lib": errors.New("bad Depends field")},
	}
	_, err := Closure[pkg](f, []pkg{"app"})
	if err == nil || err.Error() != "dependencies of lib: bad Depends field" {
		t.Errorf("error = %v, want the model's error with the package", err)
	}
}
