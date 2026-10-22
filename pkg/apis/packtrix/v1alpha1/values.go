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

package v1alpha1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/packtrix/packtrix/pkg/apis/packtrix"
)

// SecretRef points to a secret without containing it: "env:NAME" reads an
// environment variable and "file:/path" reads a file.
type SecretRef string

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
type Duration time.Duration

// MarshalJSON writes the duration as a string.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

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
	*d = Duration(parsed)
	return nil
}

// ByteSize is a size in bytes, written as a number of bytes or as a string
// with a unit: B, KB, MB, GB, TB (powers of 1000) or KiB, MiB, GiB, TiB
// (powers of 1024).
type ByteSize int64

// String formats the size with the largest binary unit that divides it
// exactly, e.g. "4GiB", or in bytes.
func (b ByteSize) String() string { return packtrix.ByteSize(b).String() }

// MarshalJSON writes the size as a string with a unit.
func (b ByteSize) MarshalJSON() ([]byte, error) { return json.Marshal(b.String()) }

// UnmarshalJSON accepts a string with a unit or a plain number of bytes.
func (b *ByteSize) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		parsed, err := packtrix.ParseByteSize(s)
		if err != nil {
			return err
		}
		*b = ByteSize(parsed)
		return nil
	}
	var n int64
	if err := json.Unmarshal(data, &n); err != nil || n < 0 {
		return fmt.Errorf("invalid size %s: use a number with a unit, e.g. \"4GiB\"", bytes.TrimSpace(data))
	}
	*b = ByteSize(n)
	return nil
}
