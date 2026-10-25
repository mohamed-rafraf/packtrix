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

package manifest

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/utils/dump"

	"github.com/packtrix/packtrix/pkg/apis/packtrix"
)

const fingerprint = "F6ECB3762474EDA9D21B7022871920D1991BC93C"

func lockPackage(name, version, repository string, why ...string) packtrix.LockPackage {
	return packtrix.LockPackage{
		Name: name, Version: version, Architecture: "amd64", Repository: repository,
		URL:    "https://archive.ubuntu.com/ubuntu/pool/main/" + name + "_" + version + "_amd64.deb",
		SHA256: strings.Repeat("ab", 32), Size: 1000, Why: why,
	}
}

// testLock is a valid lock whose lists aren't sorted.
func testLock() *packtrix.Lock {
	l := &packtrix.Lock{Spec: packtrix.LockSpec{
		Manifest: "sha256:" + strings.Repeat("0", 64),
		Catalog:  "2026.10.1",
		Targets: []packtrix.LockTarget{
			{
				Name: "ubuntu-24.04", Architecture: "arm64", Format: packtrix.RepoTypeAPT, Distribution: "Ubuntu noble",
				Repositories: []packtrix.LockRepository{
					{Name: "release", URL: "https://ports.ubuntu.com/ubuntu-ports/", Suite: "noble", Components: []string{"main"}, Release: "sha256:" + strings.Repeat("1", 64), Signer: fingerprint},
				},
				Packages: []packtrix.LockPackage{lockPackage("curl", "8.5.0", "release")},
			},
			{
				Name: "ubuntu-24.04", Architecture: "amd64", Format: packtrix.RepoTypeAPT, Distribution: "Ubuntu noble",
				Repositories: []packtrix.LockRepository{
					{Name: "updates", URL: "https://archive.ubuntu.com/ubuntu/", Suite: "noble-updates", Components: []string{"main"}, Release: "sha256:" + strings.Repeat("2", 64), Signer: fingerprint},
					{Name: "release", URL: "https://archive.ubuntu.com/ubuntu/", Suite: "noble", Components: []string{"main"}, Release: "sha256:" + strings.Repeat("1", 64), Signer: fingerprint},
				},
				Packages: []packtrix.LockPackage{
					lockPackage("libcurl4t64", "8.5.0", "updates", "curl"),
					lockPackage("curl", "8.5.0", "updates"),
				},
			},
		},
	}}
	l.Name = "test"
	return l
}

func TestMarshalLock(t *testing.T) {
	l := testLock()
	data, err := MarshalLock(l)
	if err != nil {
		t.Fatal(err)
	}
	again, err := MarshalLock(testLock())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, again) {
		t.Error("the same lock gave different bytes")
	}
	text := string(data)
	for _, want := range []string{"apiVersion: packtrix.io/v1alpha1\nkind: Lock\n", "  manifest: sha256:000"} {
		if !strings.Contains(text, want) {
			t.Errorf("lock is missing %q:\n%s", want, text)
		}
	}
	// Targets by name then architecture; repositories and packages by name.
	order := []string{"architecture: amd64", "name: curl", "name: libcurl4t64", "name: release", "name: updates", "architecture: arm64"}
	last := -1
	for _, s := range order {
		i := strings.Index(text[last+1:], s)
		if i < 0 {
			t.Fatalf("%q isn't after the previous entries:\n%s", s, text)
		}
		last += 1 + i
	}
	if strings.Contains(text, "creationTimestamp") {
		t.Errorf("lock has a timestamp:\n%s", text)
	}
	// Marshaling doesn't reorder the caller's lock.
	if l.Spec.Targets[0].Architecture != "arm64" {
		t.Error("MarshalLock sorted the caller's lock")
	}

	parsed, err := ParseLock(data)
	if err != nil {
		t.Fatal(err)
	}
	sorted := l.DeepCopy()
	sortLock(sorted)
	if !apiequality.Semantic.DeepEqual(parsed, sorted) {
		t.Errorf("round trip changed the lock:\n%s", dump.Pretty(parsed))
	}
}

func TestWriteAndLoadLockFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), LockName)
	for i, want := range []bool{true, false} {
		changed, err := WriteLockFile(path, testLock())
		if err != nil {
			t.Fatal(err)
		}
		if changed != want {
			t.Errorf("write %d: changed = %t, want %t", i+1, changed, want)
		}
	}
	changed, err := WriteLockFile(path, &packtrix.Lock{Spec: testLock().Spec})
	if err != nil || !changed {
		t.Errorf("a different lock: changed = %t, %v", changed, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Errorf("the directory holds %v, want only the lock", entries)
	}
	if _, err := LoadLockFile(path); err != nil {
		t.Errorf("LoadLockFile: %v", err)
	}

	if _, err := WriteLockFile(filepath.Join(t.TempDir(), "missing", LockName), testLock()); err == nil {
		t.Error("writing into a missing directory succeeded")
	}
}

func TestParseLockErrors(t *testing.T) {
	dir := t.TempDir()
	invalid := testLock()
	invalid.Spec.Targets[0].Packages[0].Repository = "nowhere"
	data, err := MarshalLock(invalid)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ name, data, want string }{
		{"empty", "\n", "the lock is empty"},
		{"manifest", header + "spec:\n  targets: []\n", `unsupported lock (apiVersion "packtrix.io/v1alpha1", kind "OfflineRepository"): expected apiVersion "packtrix.io/v1alpha1" and kind "Lock"`},
		{"unknown field", "apiVersion: packtrix.io/v1alpha1\nkind: Lock\nspec:\n  manifest: x\n  extra: 1\n", `unknown field "extra"`},
		{"not YAML", "apiVersion: [", "yaml: "},
		{"invalid", string(data), `spec.targets[1].packages[0].repository: unknown repository "nowhere"`},
	} {
		path := filepath.Join(dir, tt.name+".lock")
		if err := os.WriteFile(path, []byte(tt.data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadLockFile(path); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error = %v, want %q", tt.name, err, tt.want)
		}
	}
	if _, err := LoadLockFile(filepath.Join(dir, "missing.lock")); err == nil {
		t.Error("loading a missing lock succeeded")
	}
}

// The digest ignores comments and formatting, but not what the manifest
// says.
func TestDigestAndCheckLock(t *testing.T) {
	digest := func(text string) string {
		t.Helper()
		m, err := Parse([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		d, err := Digest(m)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	base := digest(header + "spec:\n  targets:\n    - distro: ubuntu-24.04\n  packages: [curl]\n")
	if !strings.HasPrefix(base, "sha256:") || len(base) != 71 {
		t.Fatalf("Digest = %q", base)
	}
	same := digest(header + "# comment\nspec:\n  packages:\n    - curl   # the CLI\n  targets:\n  - distro:\n      name: ubuntu-24.04\n")
	defaults := digest(header + "spec:\n  targets:\n    - distro: ubuntu-24.04\n      architectures: [amd64]\n  packages: [curl]\n  resolve:\n    baseline: minimal\n")
	changed := digest(header + "spec:\n  targets:\n    - distro: ubuntu-24.04\n  packages: [curl, jq]\n")
	if same != base || defaults != base {
		t.Error("comments, formatting or defaults changed the digest")
	}
	if changed == base {
		t.Error("adding a package didn't change the digest")
	}

	m, err := Parse([]byte(header + "spec:\n  targets:\n    - distro: ubuntu-24.04\n  packages: [curl]\n"))
	if err != nil {
		t.Fatal(err)
	}
	l := testLock()
	l.Spec.Manifest = base
	if err := CheckLock(l, m); err != nil {
		t.Errorf("CheckLock = %v", err)
	}
	m.Spec.Packages = append(m.Spec.Packages, packtrix.Package{Name: "jq"})
	if err := CheckLock(l, m); err == nil || err.Error() != "the lock was planned from another version of the manifest; run packtrix plan" {
		t.Errorf("CheckLock after a change = %v", err)
	}
}
