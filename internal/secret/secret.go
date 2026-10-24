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

// Package secret reads the secrets that a manifest references, such as the
// signing key or a repository password, without ever printing them.
package secret

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/packtrix/packtrix/pkg/apis/packtrix"
)

const redacted = "[redacted]"

// Value is a secret. It formats as "[redacted]" with every verb, and so does
// a struct that holds it, so that printing or logging it by mistake doesn't
// reveal it.
type Value struct {
	// get is a function because fmt prints a struct's unexported fields
	// without calling their methods, and prints a function as its address.
	get func() []byte
}

func newValue(b []byte) Value {
	return Value{func() []byte { return b }}
}

// Bytes returns the secret.
func (v Value) Bytes() []byte {
	if v.get == nil {
		return nil
	}
	return v.get()
}

// Format implements fmt.Formatter.
func (Value) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, redacted)
}

// LogValue implements slog.LogValuer.
func (Value) LogValue() slog.Value {
	return slog.StringValue(redacted)
}

// Resolve reads the secret that ref points to: an environment variable for
// "env:NAME", a file for "file:/path". A relative path is relative to dir,
// the manifest's directory. Errors name the variable or the file, never the
// secret.
func Resolve(ref packtrix.SecretRef, dir string) (Value, error) {
	if err := ref.Validate(); err != nil {
		return Value{}, fmt.Errorf("invalid secret reference: %w", err)
	}
	scheme, name, _ := strings.Cut(string(ref), ":")
	if scheme == "env" {
		s, ok := os.LookupEnv(name)
		switch {
		case !ok:
			return Value{}, fmt.Errorf("environment variable %s isn't set", name)
		case s == "":
			return Value{}, fmt.Errorf("environment variable %s is empty", name)
		}
		return newValue([]byte(s)), nil
	}
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Value{}, err
	}
	if len(b) == 0 {
		return Value{}, fmt.Errorf("%s is empty", path)
	}
	return newValue(b), nil
}
