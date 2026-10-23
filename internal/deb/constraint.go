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
	"strings"
)

// Constraint is a package version constraint from a manifest:
//
//   - "1.34.1": upstream version 1.34.1, any revision;
//   - "1.34.*": upstream version 1.34 or 1.34.x, any revision;
//   - "1.34.1-1.1": exactly that version.
//
// A constraint with an epoch, such as "1:2.0", also requires that epoch;
// without one, any epoch matches. The empty constraint matches every version.
type Constraint struct {
	text     string
	epoch    int
	hasEpoch bool
	upstream string
	revision string
	wildcard bool
}

// ParseConstraint parses a version constraint. The only wildcard is a trailing
// ".*".
func ParseConstraint(s string) (Constraint, error) {
	c := Constraint{text: s}
	if s == "" {
		return c, nil
	}
	rest, wildcard := strings.CutSuffix(s, ".*")
	c.wildcard = wildcard
	if strings.Contains(rest, "*") {
		return Constraint{}, fmt.Errorf("invalid version constraint %q: '*' is only allowed at the end, as in \"1.34.*\"", s)
	}
	v, err := ParseVersion(rest)
	if err != nil {
		return Constraint{}, fmt.Errorf("invalid version constraint %q: %w", s, err)
	}
	if c.wildcard && v.Revision != "" {
		return Constraint{}, fmt.Errorf("invalid version constraint %q: a wildcard can't be combined with a revision", s)
	}
	c.hasEpoch = strings.Contains(rest, ":")
	c.epoch, c.upstream, c.revision = v.Epoch, v.Upstream, v.Revision
	return c, nil
}

// Matches reports whether version v satisfies the constraint. Versions are
// compared by dpkg's rules, so "1.34.01" matches "1.34.1".
func (c Constraint) Matches(v Version) bool {
	switch {
	case c.text == "":
		return true
	case c.hasEpoch && v.Epoch != c.epoch:
		return false
	case c.wildcard:
		return compareString(v.Upstream, c.upstream) == 0 || strings.HasPrefix(v.Upstream, c.upstream+".")
	case compareString(v.Upstream, c.upstream) != 0:
		return false
	default:
		return c.revision == "" || compareString(v.Revision, c.revision) == 0
	}
}

func (c Constraint) String() string { return c.text }
