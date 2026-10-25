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
	"testing"
)

func parsePackage(t *testing.T, text string) (*Package, error) {
	t.Helper()
	ps := readAll(t, text)
	if len(ps) != 1 {
		t.Fatalf("got %d paragraphs, want 1", len(ps))
	}
	return ParsePackage(ps[0])
}

func TestParsePackage(t *testing.T) {
	p, err := parsePackage(t, `Package: libcurl4t64
Architecture: amd64
Version: 8.5.0-2ubuntu10.6
Priority: optional
Multi-Arch: same
Provides: libcurl4 (= 8.5.0-2ubuntu10.6)
Pre-Depends: libc6 (>= 2.34)
Depends: libbrotli1 (>= 0.6.0), libgssapi-krb5-2 (>= 1.17),
 libssl3t64 (>= 3.0.0)
Recommends: ca-certificates
Breaks: libcurl4 (<< 8.5.0-2ubuntu10.6)
Conflicts: libcurl4-gnutls-dev
Filename: pool/main/c/curl/libcurl4t64_8.5.0-2ubuntu10.6_amd64.deb
Size: 341460
SHA256: 2f4d0c5dbbd2ef0bad1a9c3a36b9e5b6e1d8bd9a1c6e9e3a3a0e4f1f1e8d6c5b
`)
	if err != nil {
		t.Fatal(err)
	}
	if p.Essential {
		t.Error("Essential is set, want it unset without an Essential field")
	}
	for _, tt := range []struct{ field, got, want string }{
		{"String", p.String(), "libcurl4t64=8.5.0-2ubuntu10.6"},
		{"Architecture", p.Architecture, "amd64"},
		{"Priority", p.Priority, "optional"},
		{"Provides", Alternatives(p.Provides).String(), "libcurl4 (= 8.5.0-2ubuntu10.6)"},
		{"Pre-Depends", format(p.PreDepends), "libc6 (>= 2.34)"},
		{"Depends", format(p.Depends), "libbrotli1 (>= 0.6.0), libgssapi-krb5-2 (>= 1.17), libssl3t64 (>= 3.0.0)"},
		{"Recommends", format(p.Recommends), "ca-certificates"},
		{"Breaks", format(p.Breaks), "libcurl4 (<< 8.5.0-2ubuntu10.6)"},
		{"Conflicts", format(p.Conflicts), "libcurl4-gnutls-dev"},
		{"Filename", p.Filename, "pool/main/c/curl/libcurl4t64_8.5.0-2ubuntu10.6_amd64.deb"},
		{"Size", fmt.Sprint(p.Size), "341460"},
		{"SHA256", p.SHA256, "2f4d0c5dbbd2ef0bad1a9c3a36b9e5b6e1d8bd9a1c6e9e3a3a0e4f1f1e8d6c5b"},
	} {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.field, tt.got, tt.want)
		}
	}
}

func TestParsePackageEssential(t *testing.T) {
	p, err := parsePackage(t, "Package: dpkg\nArchitecture: amd64\nVersion: 1.22.6ubuntu6\nEssential: yes\nPriority: required\n")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Essential || p.Priority != "required" {
		t.Errorf("Essential = %t, Priority = %q, want an essential, required package", p.Essential, p.Priority)
	}
}

func TestParsePackageErrors(t *testing.T) {
	const valid = "Package: curl\nArchitecture: amd64\nVersion: 8.5.0\n"
	for _, tt := range []struct{ text, want string }{
		{"Version: 1.0\n", "paragraph without a Package field"},
		{"Package: Curl\n", `invalid package name "Curl"`},
		{"Package: curl\nVersion: 1.0\n", `package curl: Architecture: invalid architecture ""`},
		{"Package: curl\nArchitecture: amd64\n", `package curl: Version: invalid version ""`},
		{valid + "Provides: pa | pb\n", "package curl: Provides: invalid Provides"},
		{valid + "Pre-Depends: libc6 (>= )\n", "package curl: Pre-Depends: invalid dependency"},
		{valid + "Depends: libc6,\n", "package curl: Depends: invalid dependency field"},
		{valid + "Conflicts: [amd64]\n", "package curl: Conflicts: invalid dependency"},
		{valid + "Size: -1\n", `package curl: Size: invalid size "-1"`},
		{valid + "Size: big\n", `package curl: Size: invalid size "big"`},
		{valid + "SHA256: ABC\n", `package curl: SHA256: invalid SHA-256 "ABC"`},
	} {
		_, err := parsePackage(t, tt.text)
		if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
			t.Errorf("ParsePackage(%q) = %v, want an error starting with %q", tt.text, err, tt.want)
		}
	}
}
