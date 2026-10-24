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

package secret

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packtrix/packtrix/pkg/apis/packtrix"
)

const value = "s3cret-value"

func TestResolve(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "key.asc"), []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "empty"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACKTRIX_TEST_SECRET", value)
	t.Setenv("PACKTRIX_TEST_EMPTY", "")

	for _, ref := range []packtrix.SecretRef{
		"env:PACKTRIX_TEST_SECRET",
		"file:key.asc",
		packtrix.SecretRef("file:" + filepath.Join(dir, "key.asc")),
	} {
		v, err := Resolve(ref, dir)
		if err != nil {
			t.Errorf("%s: %v", ref, err)
		} else if string(v.Bytes()) != value {
			t.Errorf("%s: got the wrong secret", ref)
		}
	}

	for _, tt := range []struct {
		ref  packtrix.SecretRef
		want string
	}{
		{"env:PACKTRIX_TEST_UNSET", "environment variable PACKTRIX_TEST_UNSET isn't set"},
		{"env:PACKTRIX_TEST_EMPTY", "environment variable PACKTRIX_TEST_EMPTY is empty"},
		{"file:empty", filepath.Join(dir, "empty") + " is empty"},
		{"file:missing", "open " + filepath.Join(dir, "missing") + ": no such file or directory"},
		// A secret pasted where its reference belongs isn't repeated.
		{value, `invalid secret reference: must be "env:NAME" or "file:/path"`},
	} {
		_, err := Resolve(tt.ref, dir)
		if err == nil || err.Error() != tt.want {
			t.Errorf("Resolve(%q) = %v, want %q", tt.ref, err, tt.want)
		}
	}
}

func TestValueIsRedacted(t *testing.T) {
	t.Setenv("PACKTRIX_TEST_SECRET", value)
	v, err := Resolve("env:PACKTRIX_TEST_SECRET", "")
	if err != nil {
		t.Fatal(err)
	}
	type exported struct{ Key Value }
	type unexported struct{ key Value }
	var printed []string
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
		for _, arg := range []any{v, &v, exported{v}, unexported{v}, []Value{v}} {
			printed = append(printed, fmt.Sprintf(verb, arg))
		}
	}
	var log bytes.Buffer
	slog.New(slog.NewJSONHandler(&log, nil)).Info("signing", "key", v)
	printed = append(printed, log.String(), fmt.Sprint(v), fmt.Sprintln(v))

	for _, out := range printed {
		for _, leak := range []string{value, strings.Trim(fmt.Sprintf("%d", []byte(value)), "[]"), fmt.Sprintf("%x", value)} {
			if strings.Contains(out, leak) {
				t.Errorf("printed %q, which reveals the secret", out)
			}
		}
	}
	if got := fmt.Sprint(v); got != "[redacted]" {
		t.Errorf("printed %q, want [redacted]", got)
	}
	if (Value{}).Bytes() != nil {
		t.Error("the zero Value has bytes")
	}
}
