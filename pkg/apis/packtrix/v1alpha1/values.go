package v1alpha1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SecretRef points to a secret without containing it: "env:NAME" reads an
// environment variable and "file:/path" reads a file.
type SecretRef string

var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

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

// VersionConstraint is a package version constraint such as "1.34.*". In
// YAML it must be a string: an unquoted 1.30 is a number, and becomes 1.3.
type VersionConstraint string

// UnmarshalJSON rejects numbers with a hint to quote the value.
func (v *VersionConstraint) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("version %s must be a quoted string, e.g. \"1.34.*\" (unquoted, 1.30 would be read as the number 1.3)", bytes.TrimSpace(data))
	}
	*v = VersionConstraint(s)
	return nil
}

// Duration is a time.Duration written as a string such as "72h".
type Duration struct {
	time.Duration
}

// MarshalJSON writes the duration as a string.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

// UnmarshalJSON parses strings accepted by time.ParseDuration.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("duration %s must be a string such as \"72h\"", bytes.TrimSpace(data))
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: use units such as \"30m\" or \"72h\"", s)
	}
	d.Duration = parsed
	return nil
}

// ByteSize is a size in bytes, written as a number of bytes or as a string
// with a unit: B, KB, MB, GB, TB (powers of 1000) or KiB, MiB, GiB, TiB
// (powers of 1024).
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

// ParseByteSize parses sizes such as "4GiB", "500MB" or "1.5GiB".
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

// MarshalJSON writes the size as a string with a unit.
func (b ByteSize) MarshalJSON() ([]byte, error) { return json.Marshal(b.String()) }

// UnmarshalJSON accepts a string with a unit or a plain number of bytes.
func (b *ByteSize) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		parsed, err := ParseByteSize(s)
		if err != nil {
			return err
		}
		*b = parsed
		return nil
	}
	var n int64
	if err := json.Unmarshal(data, &n); err != nil || n < 0 {
		return fmt.Errorf("invalid size %s: use a number with a unit, e.g. \"4GiB\"", bytes.TrimSpace(data))
	}
	*b = ByteSize(n)
	return nil
}
