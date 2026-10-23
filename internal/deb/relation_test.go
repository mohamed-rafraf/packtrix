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
	"reflect"
	"strings"
	"testing"
)

func TestParseDependencies(t *testing.T) {
	tests := []struct {
		field string
		// want is the parsed field, formatted: dependencies joined by ", ".
		want string
	}{
		{"", ""},
		{"  ", ""},
		// Debian Policy 7.1.
		{"libc6 (>= 2.2.1), exim | mail-transport-agent", "libc6 (>= 2.2.1), exim | mail-transport-agent"},
		{"python3:any", "python3:any"},
		{"python3:any (>= 3.8~)", "python3:any (>= 3.8~)"},
		// Every operator, with and without spaces, and the obsolete < and >.
		{"pa (<< 1), pb (<= 1), pc (= 1), pd (>= 1), pe (>> 1)", "pa (<< 1), pb (<= 1), pc (= 1), pd (>= 1), pe (>> 1)"},
		{"pa(>=1.0),pb (>>2)", "pa (>= 1.0), pb (>> 2)"},
		{"pa (< 1), pb (> 1)", "pa (<= 1), pb (>= 1)"},
		// Fields folded over several lines, as in a Packages file.
		{"libc6 (>= 2.34),\n libssl3t64 (>= 3.0.0)\n | libssl3", "libc6 (>= 2.34), libssl3t64 (>= 3.0.0) | libssl3"},
		{"libstdc++6 (>= 13.1), g++-13:amd64", "libstdc++6 (>= 13.1), g++-13:amd64"},
	}
	for _, tt := range tests {
		deps, err := ParseDependencies(tt.field)
		if err != nil {
			t.Errorf("ParseDependencies(%q): %v", tt.field, err)
			continue
		}
		if got := format(deps); got != tt.want {
			t.Errorf("ParseDependencies(%q) = %q, want %q", tt.field, got, tt.want)
		}
	}
}

func TestParseDependenciesFields(t *testing.T) {
	deps, err := ParseDependencies("libc6 (>= 2.34), python3:any | python3-minimal")
	if err != nil {
		t.Fatal(err)
	}
	want := []Alternatives{
		{{Name: "libc6", Op: OpGreaterEqual, Version: Version{Upstream: "2.34"}}},
		{{Name: "python3", Arch: "any"}, {Name: "python3-minimal"}},
	}
	if !reflect.DeepEqual(deps, want) {
		t.Errorf("got %+v, want %+v", deps, want)
	}
}

func TestParseDependenciesErrors(t *testing.T) {
	tests := map[string]string{
		"aa,,bb":            "empty dependency",
		"aa, bb,":           "empty dependency",
		"aa | | bb":         "empty alternative",
		"aa |":              "empty alternative",
		"A":                 "invalid package name",
		"a":                 "invalid package name", // names have at least two characters
		"libc6:":            "invalid architecture qualifier",
		"libc6:AMD64":       "invalid architecture qualifier",
		"libc6 (>= 2.34":    "missing ')'",
		"libc6 (>= 2.34) x": `unexpected "x" after ')'`,
		"libc6 ((>= 1))":    "unexpected '('",
		"libc6 (2.34)":      "needs an operator",
		"libc6 (=> 2.34)":   "invalid version",
		"libc6 (>= )":       "empty upstream version",
		"libc6 [amd64]":     "only valid in source packages",
		"libc6 <!nocheck>":  "only valid in source packages",
	}
	for field, want := range tests {
		_, err := ParseDependencies(field)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseDependencies(%q) error = %v, want it to contain %q", field, err, want)
		}
	}
}

func TestParseProvides(t *testing.T) {
	// Debian Policy 7.5, plus a versioned Provides.
	provides, err := ParseProvides("mail-transport-agent, libfoo-api (= 1.2)")
	if err != nil {
		t.Fatal(err)
	}
	want := []Relation{
		{Name: "mail-transport-agent"},
		{Name: "libfoo-api", Op: OpEqual, Version: Version{Upstream: "1.2"}},
	}
	if !reflect.DeepEqual(provides, want) {
		t.Errorf("got %+v, want %+v", provides, want)
	}

	for field, wantErr := range map[string]string{
		"foo | bar":    "alternatives",
		"foo (>= 1.0)": "only '=' versions",
	} {
		if _, err := ParseProvides(field); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("ParseProvides(%q) error = %v, want it to contain %q", field, err, wantErr)
		}
	}
}

func TestSatisfiedBy(t *testing.T) {
	tests := []struct {
		relation string
		version  string
		want     bool
	}{
		{"pkg", "0.1", true},
		{"pkg (<< 2.0)", "1.9", true},
		{"pkg (<< 2.0)", "2.0", false},
		{"pkg (<= 2.0)", "2.0", true},
		{"pkg (<= 2.0)", "2.0+b1", false},
		{"pkg (= 2.0-1)", "2.0-1", true},
		{"pkg (= 2.0-1)", "2.0-2", false},
		{"pkg (>= 2.0)", "2.0~rc1", false},
		{"pkg (>= 2.0)", "1:1.0", true},
		{"pkg (>> 2.0)", "2.0", false},
		{"pkg (>> 2.0)", "2.0.1", true},
		{"libssl3 (>= 3.0.13-0ubuntu3.5)", "3.0.13-0ubuntu3.4", false},
	}
	for _, tt := range tests {
		deps, err := ParseDependencies(tt.relation)
		if err != nil {
			t.Fatal(err)
		}
		if got := deps[0][0].SatisfiedBy(mustVersion(t, tt.version)); got != tt.want {
			t.Errorf("%q satisfied by %q = %v, want %v", tt.relation, tt.version, got, tt.want)
		}
	}
}

// Parsing must never panic, and a parsed field must format to a field that
// parses back to the same dependencies.
func FuzzParseDependencies(f *testing.F) {
	for _, s := range []string{
		"libc6 (>= 2.2.1), exim | mail-transport-agent",
		"python3:any (>= 3.8~), pa(<<1)",
		"a,,b", "libc6 [amd64]", "x (", "x )", "a | ", "(", ")", "|", ",",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, field string) {
		deps, err := ParseDependencies(field)
		if err != nil {
			return
		}
		again, err := ParseDependencies(format(deps))
		if err != nil {
			t.Fatalf("ParseDependencies(%q) formats as %q, which fails to parse: %v", field, format(deps), err)
		}
		if format(again) != format(deps) {
			t.Fatalf("round trip of %q: %q != %q", field, format(again), format(deps))
		}
	})
}

func format(deps []Alternatives) string {
	parts := make([]string, len(deps))
	for i, alts := range deps {
		parts[i] = alts.String()
	}
	return strings.Join(parts, ", ")
}
