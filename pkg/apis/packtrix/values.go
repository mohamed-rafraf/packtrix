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

package packtrix

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// SecretRef points to a secret without containing it: "env:NAME" reads an
// environment variable and "file:/path" reads a file.
type SecretRef string

// SecretRefPattern matches the references that SecretRef.Validate accepts.
const SecretRefPattern = `^(env:` + envName + `|file:.+)$`

const envName = `[A-Za-z_][A-Za-z0-9_]*`

var envNameRE = regexp.MustCompile(`^` + envName + `$`)

// Validate checks the reference's syntax.
func (r SecretRef) Validate() error {
	scheme, value, ok := strings.Cut(string(r), ":")
	switch {
	case !ok || (scheme != "env" && scheme != "file"):
		return errors.New(`must be "env:NAME" or "file:/path"`)
	case scheme == "env" && !envNameRE.MatchString(value):
		return fmt.Errorf("invalid environment variable name %q", value)
	case scheme == "file" && value == "":
		return errors.New(`missing path after "file:"`)
	}
	return nil
}

// VersionConstraint is a package version constraint such as "1.34.*".
type VersionConstraint string

// ByteSize is a size in bytes.
type ByteSize int64

// Size units.
const (
	KiB ByteSize = 1 << (10 * (iota + 1))
	MiB
	GiB
	TiB
)

var byteUnits = map[string]ByteSize{
	"B":   1,
	"KB":  1000,
	"MB":  1000 * 1000,
	"GB":  1000 * 1000 * 1000,
	"TB":  1000 * 1000 * 1000 * 1000,
	"KiB": KiB,
	"MiB": MiB,
	"GiB": GiB,
	"TiB": TiB,
}

// ParseByteSize parses sizes such as "4GiB", "500MB" or "1.5GiB": a number
// with a unit, either B, KB, MB, GB, TB (powers of 1000) or KiB, MiB, GiB,
// TiB (powers of 1024).
func ParseByteSize(s string) (ByteSize, error) {
	s = strings.TrimSpace(s)
	i := strings.IndexFunc(s, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if i <= 0 {
		return 0, fmt.Errorf("invalid size %q: use a number with a unit, e.g. \"4GiB\"", s)
	}
	unit, ok := byteUnits[strings.TrimSpace(s[i:])]
	if !ok {
		return 0, fmt.Errorf("invalid size %q: unknown unit (use B, KB, MB, GB, TB, KiB, MiB, GiB or TiB)", s)
	}
	n, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	size := n * float64(unit)
	if size > math.MaxInt64 {
		return 0, fmt.Errorf("size %q is too large", s)
	}
	return ByteSize(math.Round(size)), nil
}

// String formats the size with the largest binary unit that divides it
// exactly, e.g. "4GiB", or in bytes.
func (b ByteSize) String() string {
	for _, u := range []struct {
		name string
		size ByteSize
	}{{"TiB", TiB}, {"GiB", GiB}, {"MiB", MiB}, {"KiB", KiB}} {
		if b != 0 && b%u.size == 0 {
			return fmt.Sprintf("%d%s", b/u.size, u.name)
		}
	}
	return fmt.Sprintf("%dB", b)
}
