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

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSync(t *testing.T) {
	path, caFile := planFixture(t, "[curl]")
	cacheDir := t.TempDir()
	if _, err := run(t, "plan", "-f", path, "--ca-file", caFile, "--cache-dir", cacheDir); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "sync", "-f", path, "--ca-file", caFile, "--cache-dir", cacheDir, "--base-url", "http://repo.internal")
	if err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(filepath.Dir(path), "repos", "current")
	for _, want := range []string{
		"Downloaded internal (amd64): 2 packages, 2 new, 1.6 MiB\n",
		"internal  test   amd64          2         1.6 MiB\n",
		"Published " + current + " (snapshot ",
		"Each machine needs two files: see " + filepath.Join(current, "clients", "README") + ".\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "--base-url") {
		t.Errorf("output asks for --base-url, which is set:\n%s", out)
	}
	sources, err := os.ReadFile(filepath.Join(current, "clients", "internal.sources"))
	if err != nil || !strings.Contains(string(sources), "URIs: http://repo.internal/apt/internal/\nSuites: test\n") {
		t.Errorf("sources = %q, %v", sources, err)
	}

	// Without --base-url, the client files use the placeholder, and sync
	// says so.
	out, err = run(t, "sync", "-f", path, "--ca-file", caFile, "--cache-dir", cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "set --base-url to where "+current+" is served") {
		t.Errorf("output doesn't ask for --base-url:\n%s", out)
	}
}

func TestSyncRefuses(t *testing.T) {
	path, caFile := planFixture(t, "[curl]")
	lock := filepath.Join(filepath.Dir(path), "packtrix.lock")
	if _, err := run(t, "sync", "-f", path, "--ca-file", caFile, "--cache-dir", t.TempDir()); err == nil || !strings.Contains(err.Error(), lock) {
		t.Errorf("without a lock: error = %v", err)
	}

	if _, err := run(t, "plan", "-f", path, "--ca-file", caFile, "--cache-dir", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ old, new, want string }{
		{"packages: [curl]", "packages: [curl, libcurl4]", lock + ": the lock was planned from another version of the manifest; run packtrix plan"},
		{"      key: file:signing.asc\n", "", "publish.signing.key is required to sign the repositories"},
		{"key: file:signing.asc", "key: file:missing.asc", "publish.signing.key: open "},
		{"key: file:signing.asc", "key: file:archive.asc", "publish.signing.key: key "},
	} {
		changed := strings.Replace(string(manifest), tt.old, tt.new, 1)
		if tt.old == "packages: [curl]" {
			if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
				t.Fatal(err)
			}
		} else {
			// The lock must still match: plan the changed manifest first.
			if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := run(t, "plan", "-f", path, "--ca-file", caFile, "--cache-dir", t.TempDir()); err != nil {
				t.Fatal(err)
			}
		}
		_, err := run(t, "sync", "-f", path, "--ca-file", caFile, "--cache-dir", t.TempDir())
		if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
			t.Errorf("%q: error = %v, want %q", tt.new, err, tt.want)
		}
		if err := os.WriteFile(path, manifest, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
