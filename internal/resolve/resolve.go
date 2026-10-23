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

// Package resolve computes the dependency closure of a target: every package
// its machines could need to install the listed packages, beyond what they
// already have. It doesn't know any package format; a Model translates APT or
// RPM dependencies into Needs.
//
// Packtrix builds repositories, not installations, so the closure only has to
// be sufficient: the machines' own apt or dnf choose among what it contains.
// Where a choice is unclear, the engine includes rather than omits.
package resolve

import (
	"fmt"
	"slices"
	"strings"
)

// Package is a package version of one target. The engine only compares
// packages and prints them; their meaning belongs to the Model.
type Package interface {
	comparable
	String() string
}

// Model describes the packages of one target in one package format.
type Model[P Package] interface {
	// Needs returns what p requires. It is called once per package added to
	// the closure.
	Needs(p P) ([]Need[P], error)
	// InBaseline reports whether p is already on the machines, so that the
	// closure doesn't need to include it.
	InBaseline(p P) bool
}

// Need is one requirement of a package, such as "libc6 (>= 2.34)".
type Need[P Package] struct {
	// Text describes the need in error messages and explanations.
	Text string
	// Alternatives are groups of packages that satisfy the need, each group
	// ordered by preference. The need is satisfied when any package of any
	// group is in the closure or the baseline. Otherwise the engine adds the
	// first package of the first non-empty group, or of every non-empty group
	// when All is set. A need without any package can't be satisfied.
	Alternatives [][]P
	All          bool
	// Condition, when set, limits the need to closures that contain, or
	// baselines that have, one of these packages. It expresses an RPM
	// "(a if b)", or an upgrade that a conflict with a baseline package forces.
	Condition []P
}

// Reason records why a package is in the closure.
type Reason[P Package] struct {
	// By is the package that needed it, or nil for a listed package.
	By *P
	// Need is the requirement it satisfies; empty for a listed package.
	Need string
}

// Result is the closure of the listed packages.
type Result[P Package] struct {
	// Packages are the closure's packages, in the order they were added:
	// the listed packages first.
	Packages []P
	reasons  map[P]Reason[P]
}

// Why returns the chain that brought p into the closure, from p to the listed
// package that started it, e.g. "libfoo1 ← nginx ← listed", or "" when p
// isn't in the closure. The chain always ends: a package is only ever needed
// by one added before it.
func (r *Result[P]) Why(p P) string {
	if _, ok := r.reasons[p]; !ok {
		return ""
	}
	parts := []string{p.String()}
	for by := r.reasons[p].By; by != nil; by = r.reasons[*by].By {
		parts = append(parts, (*by).String())
	}
	return strings.Join(append(parts, "listed"), " ← ")
}

// Reason returns why p is in the closure.
func (r *Result[P]) Reason(p P) (Reason[P], bool) {
	reason, ok := r.reasons[p]
	return reason, ok
}

// Failure is a need that no package satisfies.
type Failure[P Package] struct {
	// Path runs from the listed package to the package with the need.
	Path []P
	Need string
}

func (f Failure[P]) String() string {
	names := make([]string, len(f.Path))
	for i, p := range f.Path {
		names[i] = p.String()
	}
	return fmt.Sprintf("%s needs %s, which no package provides", strings.Join(names, " → "), f.Need)
}

// UnresolvedError lists every need that couldn't be satisfied.
type UnresolvedError[P Package] struct {
	Failures []Failure[P]
}

func (e *UnresolvedError[P]) Error() string {
	if len(e.Failures) == 1 {
		return e.Failures[0].String()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d unresolved dependencies:", len(e.Failures))
	for _, f := range e.Failures {
		b.WriteString("\n  - ")
		b.WriteString(f.String())
	}
	return b.String()
}

// Closure returns the listed packages and everything they need, directly or
// indirectly, that the baseline doesn't have. Listed packages are always
// included, even when the baseline has them. When some needs can't be
// satisfied, it returns an *UnresolvedError listing all of them.
//
// The result only depends on the model's answers and the order of the listed
// packages.
func Closure[P Package](m Model[P], listed []P) (*Result[P], error) {
	c := &closure[P]{model: m, reasons: map[P]Reason[P]{}}
	for _, p := range listed {
		c.add(p, Reason[P]{})
	}
	for {
		for c.next < len(c.packages) {
			p := c.packages[c.next]
			c.next++
			needs, err := m.Needs(p)
			if err != nil {
				return nil, fmt.Errorf("dependencies of %s: %w", p, err)
			}
			for _, n := range needs {
				c.pending = append(c.pending, pending[P]{by: p, need: n})
			}
			c.settle()
		}
		// A condition may have become true after its need was looked at.
		if !c.settle() {
			break
		}
	}
	if len(c.failures) > 0 {
		return nil, &UnresolvedError[P]{Failures: c.failures}
	}
	return &Result[P]{Packages: c.packages, reasons: c.reasons}, nil
}

type pending[P Package] struct {
	by   P
	need Need[P]
}

type closure[P Package] struct {
	model    Model[P]
	packages []P
	next     int // packages before next have had their needs collected
	reasons  map[P]Reason[P]
	pending  []pending[P]
	failures []Failure[P]
}

func (c *closure[P]) has(p P) bool {
	_, ok := c.reasons[p]
	return ok
}

func (c *closure[P]) add(p P, reason Reason[P]) {
	if c.has(p) {
		return
	}
	c.reasons[p] = reason
	c.packages = append(c.packages, p)
}

func (c *closure[P]) present(p P) bool {
	return c.has(p) || c.model.InBaseline(p)
}

// settle handles every pending need whose condition holds, and keeps the
// others for later. It reports whether it handled any.
func (c *closure[P]) settle() bool {
	var waiting []pending[P]
	handled := false
	for _, pn := range c.pending {
		if len(pn.need.Condition) > 0 && !slices.ContainsFunc(pn.need.Condition, c.present) {
			waiting = append(waiting, pn)
			continue
		}
		handled = true
		c.satisfy(pn.by, pn.need)
	}
	c.pending = waiting
	return handled
}

func (c *closure[P]) satisfy(by P, n Need[P]) {
	for _, group := range n.Alternatives {
		if slices.ContainsFunc(group, c.present) {
			return
		}
	}
	added := false
	for _, group := range n.Alternatives {
		if len(group) == 0 {
			continue
		}
		c.add(group[0], Reason[P]{By: &by, Need: n.Text})
		added = true
		if !n.All {
			break
		}
	}
	if !added {
		c.failures = append(c.failures, Failure[P]{Path: c.path(by), Need: n.Text})
	}
}

// path returns the chain from the listed package that brought p in, to p.
func (c *closure[P]) path(p P) []P {
	path := []P{p}
	for c.reasons[p].By != nil {
		p = *c.reasons[p].By
		path = append(path, p)
	}
	slices.Reverse(path)
	return path
}
