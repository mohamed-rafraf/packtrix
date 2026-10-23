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
	"fmt"
	"regexp"
	"strings"
)

// Op is the operator of a versioned relation, such as >= in "libc6 (>= 2.34)".
type Op int

// Relation operators. OpNone means the relation isn't versioned.
const (
	OpNone         Op = iota
	OpLess            // <<
	OpLessEqual       // <=
	OpEqual           // =
	OpGreaterEqual    // >=
	OpGreater         // >>
)

func (o Op) String() string {
	switch o {
	case OpLess:
		return "<<"
	case OpLessEqual:
		return "<="
	case OpEqual:
		return "="
	case OpGreaterEqual:
		return ">="
	case OpGreater:
		return ">>"
	default:
		return ""
	}
}

// operators are tried in order, so the two-character ones come first. dpkg
// still accepts the obsolete < and >, which mean <= and >=.
var operators = []struct {
	text string
	op   Op
}{
	{"<<", OpLess}, {"<=", OpLessEqual}, {">=", OpGreaterEqual}, {">>", OpGreater},
	{"=", OpEqual}, {"<", OpLessEqual}, {">", OpGreaterEqual},
}

var (
	packageNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]+$`)
	archRE        = regexp.MustCompile(`^[a-z0-9-]+$`)
)

// Relation is one package in a dependency field, such as "libc6 (>= 2.34)" or
// "python3:any".
type Relation struct {
	Name string
	// Arch is the architecture qualifier after ':', such as "any" or "amd64",
	// or empty.
	Arch string
	// Op and Version restrict the version. Version is only set when Op isn't
	// OpNone.
	Op      Op
	Version Version
}

// SatisfiedBy reports whether version v meets the relation's version
// restriction. An unversioned relation is satisfied by every version.
func (r Relation) SatisfiedBy(v Version) bool {
	c := Compare(v, r.Version)
	switch r.Op {
	case OpLess:
		return c < 0
	case OpLessEqual:
		return c <= 0
	case OpEqual:
		return c == 0
	case OpGreaterEqual:
		return c >= 0
	case OpGreater:
		return c > 0
	default:
		return true
	}
}

func (r Relation) String() string {
	s := r.Name
	if r.Arch != "" {
		s += ":" + r.Arch
	}
	if r.Op != OpNone {
		s += " (" + r.Op.String() + " " + r.Version.String() + ")"
	}
	return s
}

// Alternatives are relations separated by "|": any one of them satisfies the
// dependency.
type Alternatives []Relation

func (a Alternatives) String() string {
	parts := make([]string, len(a))
	for i, r := range a {
		parts[i] = r.String()
	}
	return strings.Join(parts, " | ")
}

// ParseDependencies parses a dependency field of a binary package: Depends,
// Pre-Depends, Recommends, Suggests, Enhances, Breaks or Conflicts. It returns
// one entry per comma-separated dependency, all of which must be satisfied.
// An empty field has no dependencies.
func ParseDependencies(field string) ([]Alternatives, error) {
	if strings.TrimSpace(field) == "" {
		return nil, nil
	}
	clauses := strings.Split(field, ",")
	deps := make([]Alternatives, 0, len(clauses))
	for _, clause := range clauses {
		if strings.TrimSpace(clause) == "" {
			return nil, fmt.Errorf("invalid dependency field %q: empty dependency between commas", field)
		}
		var alts Alternatives
		for _, text := range strings.Split(clause, "|") {
			r, err := parseRelation(text)
			if err != nil {
				return nil, err
			}
			alts = append(alts, r)
		}
		deps = append(deps, alts)
	}
	return deps, nil
}

// ParseProvides parses a Provides field: comma-separated relations without
// alternatives, versioned only with "=".
func ParseProvides(field string) ([]Relation, error) {
	deps, err := ParseDependencies(field)
	if err != nil {
		return nil, err
	}
	provides := make([]Relation, 0, len(deps))
	for _, alts := range deps {
		if len(alts) > 1 {
			return nil, fmt.Errorf("invalid Provides %q: alternatives with '|' aren't allowed", alts)
		}
		if r := alts[0]; r.Op != OpNone && r.Op != OpEqual {
			return nil, fmt.Errorf("invalid Provides %q: only '=' versions are allowed", r)
		}
		provides = append(provides, alts[0])
	}
	return provides, nil
}

// parseRelation parses "name[:arch] [(op version)]".
func parseRelation(text string) (Relation, error) {
	s := strings.TrimSpace(text)
	fail := func(reason string) (Relation, error) {
		return Relation{}, fmt.Errorf("invalid dependency %q: %s", s, reason)
	}
	if s == "" {
		return fail("empty alternative around '|'")
	}

	head, restriction, versioned := strings.Cut(s, "(")
	head = strings.TrimSpace(head)
	if strings.ContainsAny(head, "[]<>!") {
		return fail("architecture restrictions and build profiles are only valid in source packages")
	}
	var r Relation
	name, arch, qualified := strings.Cut(head, ":")
	switch {
	case !packageNameRE.MatchString(name):
		return fail(fmt.Sprintf("invalid package name %q", name))
	case qualified && !archRE.MatchString(arch):
		return fail(fmt.Sprintf("invalid architecture qualifier %q", arch))
	}
	r.Name, r.Arch = name, arch
	if !versioned {
		return r, nil
	}

	inner, after, closed := strings.Cut(restriction, ")")
	switch {
	case !closed:
		return fail("missing ')'")
	case strings.Contains(inner, "("):
		return fail("unexpected '('")
	case strings.TrimSpace(after) != "":
		return fail(fmt.Sprintf("unexpected %q after ')'", strings.TrimSpace(after)))
	}
	inner = strings.TrimSpace(inner)
	for _, o := range operators {
		if rest, ok := strings.CutPrefix(inner, o.text); ok {
			v, err := ParseVersion(strings.TrimSpace(rest))
			if err != nil {
				return fail(err.Error())
			}
			r.Op, r.Version = o.op, v
			return r, nil
		}
	}
	return fail("the version needs an operator: <<, <=, =, >= or >>")
}
