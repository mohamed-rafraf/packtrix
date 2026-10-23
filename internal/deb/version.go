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

// Package deb implements the parts of the Debian package format that
// dependency resolution needs: versions and how they sort, dependency fields
// such as Depends and Provides, the packages of a Packages index, and the
// version constraints of a manifest.
package deb

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"
)

// Version is a Debian package version, [epoch:]upstream_version[-debian_revision],
// as described in deb-version(7).
type Version struct {
	Epoch    int
	Upstream string
	// Revision is empty when the version has none, which sorts like "0".
	Revision string
}

// ParseVersion parses a version such as "1.34.1-1.1" or "1:2.38-3ubuntu1".
func ParseVersion(s string) (Version, error) {
	var v Version
	rest := s
	if epoch, after, ok := strings.Cut(rest, ":"); ok {
		n, err := strconv.ParseUint(epoch, 10, 31)
		if err != nil {
			return Version{}, fmt.Errorf("invalid version %q: the epoch before ':' must be a number", s)
		}
		v.Epoch = int(n)
		rest = after
	}
	if i := strings.LastIndexByte(rest, '-'); i >= 0 {
		v.Upstream, v.Revision = rest[:i], rest[i+1:]
		if v.Revision == "" {
			return Version{}, fmt.Errorf("invalid version %q: empty revision after '-'", s)
		}
	} else {
		v.Upstream = rest
	}
	if v.Upstream == "" {
		return Version{}, fmt.Errorf("invalid version %q: empty upstream version", s)
	}
	if !isDigit(rune(v.Upstream[0])) {
		return Version{}, fmt.Errorf("invalid version %q: the upstream version must start with a digit", s)
	}
	// Upstream versions may contain '-' (when there is a revision) and ':'
	// (when there is an epoch), which the splitting above already allows for.
	if c, ok := invalidChar(v.Upstream, ".+~-:"); ok {
		return Version{}, fmt.Errorf("invalid version %q: character %q isn't allowed", s, c)
	}
	if c, ok := invalidChar(v.Revision, ".+~"); ok {
		return Version{}, fmt.Errorf("invalid version %q: character %q isn't allowed in the revision", s, c)
	}
	return v, nil
}

// invalidChar returns the first character of s that is neither alphanumeric
// nor one of extra.
func invalidChar(s, extra string) (rune, bool) {
	for _, c := range s {
		if !isDigit(c) && !isLetter(c) && !strings.ContainsRune(extra, c) {
			return c, true
		}
	}
	return 0, false
}

// String formats the version as dpkg does: without a zero epoch, unless the
// upstream version contains ':' and would otherwise be read as an epoch.
func (v Version) String() string {
	s := v.Upstream
	if v.Epoch != 0 || strings.Contains(v.Upstream, ":") {
		s = strconv.Itoa(v.Epoch) + ":" + s
	}
	if v.Revision != "" {
		s += "-" + v.Revision
	}
	return s
}

// Compare returns -1, 0 or +1 when a sorts before, the same as, or after b, by
// dpkg's rules: the epoch first, then the upstream version, then the revision.
func Compare(a, b Version) int {
	if c := cmp.Compare(a.Epoch, b.Epoch); c != 0 {
		return c
	}
	if c := compareString(a.Upstream, b.Upstream); c != 0 {
		return c
	}
	return compareString(a.Revision, b.Revision)
}

// compareString compares two upstream versions, or two revisions, with dpkg's
// algorithm (verrevcmp): the strings are split into alternating runs of
// non-digits, compared character by character with order, and digits,
// compared as numbers of any length.
func compareString(a, b string) int {
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		for (i < len(a) && !isDigit(rune(a[i]))) || (j < len(b) && !isDigit(rune(b[j]))) {
			if ac, bc := order(a, i), order(b, j); ac != bc {
				return cmp.Compare(ac, bc)
			}
			i++
			j++
		}
		for i < len(a) && a[i] == '0' {
			i++
		}
		for j < len(b) && b[j] == '0' {
			j++
		}
		// Without leading zeros, the longer number is larger; numbers of the
		// same length compare by their first different digit.
		firstDiff := 0
		for i < len(a) && j < len(b) && isDigit(rune(a[i])) && isDigit(rune(b[j])) {
			if firstDiff == 0 {
				firstDiff = cmp.Compare(a[i], b[j])
			}
			i++
			j++
		}
		if i < len(a) && isDigit(rune(a[i])) {
			return 1
		}
		if j < len(b) && isDigit(rune(b[j])) {
			return -1
		}
		if firstDiff != 0 {
			return firstDiff
		}
	}
	return 0
}

// order is the weight of the character at s[i] in a run of non-digits: '~'
// sorts before everything, even the end of the string, letters sort after
// the end, and other characters after letters.
func order(s string, i int) int {
	if i >= len(s) {
		return 0
	}
	c := rune(s[i])
	switch {
	case isDigit(c):
		return 0
	case isLetter(c):
		return int(c)
	case c == '~':
		return -1
	default:
		return int(c) + 256
	}
}

func isDigit(c rune) bool { return c >= '0' && c <= '9' }

func isLetter(c rune) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
