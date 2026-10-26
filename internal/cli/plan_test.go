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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packtrix/packtrix/internal/plan"
	"github.com/packtrix/packtrix/internal/repo/apt/apttest"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
)

// planFixture writes a manifest whose target reads a local HTTPS archive,
// and returns the manifest and the archive's CA file.
func planFixture(t *testing.T, packages string) (manifest, caFile string) {
	t.Helper()
	signer, _ := apttest.Key(t)
	a := apttest.New(t, signer, true)
	// The package files, of the sizes plan prints.
	deb := func(name string, size int, fields string) string {
		filename := "pool/main/" + name + "_8.1_amd64.deb"
		content := bytes.Repeat([]byte(name), size/len(name)+1)[:size]
		a.Write("debian/"+filename, content)
		sum := sha256.Sum256(content)
		return fmt.Sprintf("Package: %s\nVersion: 8.1\nArchitecture: amd64\n%sFilename: %s\nSize: %d\nSHA256: %s\n\n",
			name, fields, filename, size, hex.EncodeToString(sum[:]))
	}
	a.Publish(apttest.Suite{
		Dir:          "debian/dists/test",
		Fields:       "Origin: Debian\nSuite: test\nCodename: test\n",
		Compressions: []string{".gz"},
		Indexes:      map[string]string{"main/binary-amd64/Packages": deb("curl", 230000, "Depends: libcurl4\n") + deb("libcurl4", 1500000, "")},
	})
	dir := t.TempDir()
	public, err := signer.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "archive.asc"), public, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "signing.asc"), apttest.PrivateKey(t), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest = filepath.Join(dir, "packtrix.yaml")
	data := `apiVersion: packtrix.io/v1alpha1
kind: OfflineRepository
metadata:
  name: test
spec:
  targets:
    - name: internal
      repos:
        - name: release
          type: apt
          url: ` + a.URL() + `/debian/
          suite: test
          components: [main]
          gpgKey: archive.asc
  packages: ` + packages + `
  resolve:
    baseline: none
  publish:
    signing:
      key: file:signing.asc
`
	if err := os.WriteFile(manifest, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return manifest, a.CAFile()
}

func TestPlan(t *testing.T) {
	manifest, caFile := planFixture(t, "[curl]")
	out, err := run(t, "plan", "-f", manifest, "--ca-file", caFile, "--cache-dir", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Reading internal (amd64): release, https://",
		"internal (amd64): Debian test, 2 packages, 1.6 MiB\n",
		"  curl      8.1      224.6 KiB  listed\n",
		"  libcurl4  8.1      1.4 MiB    curl\n",
		"1 target(s), 2 package files to download, 1.6 MiB\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}

	// The progress goes to stderr, so stdout is only the JSON.
	cmd := NewRootCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"plan", "-f", manifest, "--ca-file", caFile, "-o", "json", "--cache-dir", t.TempDir()})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var p plan.Plan
	if err := json.Unmarshal(stdout.Bytes(), &p); err != nil {
		t.Fatalf("%v:\n%s", err, stdout.String())
	}
	if !strings.HasPrefix(stderr.String(), "Reading internal (amd64)") {
		t.Errorf("stderr = %q, want the progress", stderr.String())
	}
	if len(p.Targets) != 1 || len(p.Targets[0].Packages) != 2 || p.Targets[0].Packages[1].Why[0] != "curl" {
		t.Errorf("plan = %+v", p)
	}
}

// plan writes the lock next to the manifest, and planning the same thing
// again writes the same bytes.
func TestPlanWritesLock(t *testing.T) {
	path, caFile := planFixture(t, "[curl]")
	lockPath := filepath.Join(filepath.Dir(path), "packtrix.lock")
	cacheDir := t.TempDir()
	var locks [][]byte
	for i, want := range []string{"Wrote " + lockPath, lockPath + " is unchanged"} {
		out, err := run(t, "plan", "-f", path, "--ca-file", caFile, "--cache-dir", cacheDir)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, want+"\n") {
			t.Errorf("run %d: output is missing %q:\n%s", i+1, want, out)
		}
		data, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		locks = append(locks, data)
	}
	if !bytes.Equal(locks[0], locks[1]) {
		t.Error("planning twice wrote different locks")
	}
	// The verified Release and its index are in the cache.
	if cached, _ := filepath.Glob(filepath.Join(cacheDir, "indexes", "sha256", "*", "*")); len(cached) != 2 {
		t.Errorf("the cache holds %q, want the Release and the index", cached)
	}
	l, err := manifest.LoadLockFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := manifest.CheckLock(l, m); err != nil {
		t.Error(err)
	}
	if len(l.Spec.Targets) != 1 || len(l.Spec.Targets[0].Packages) != 2 || l.Spec.Catalog == "" {
		t.Errorf("lock = %+v", l)
	}

	other := filepath.Join(t.TempDir(), "other.lock")
	if _, err := run(t, "plan", "-f", path, "--ca-file", caFile, "--lock", other, "--cache-dir", cacheDir); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(other); err != nil || !bytes.Equal(data, locks[0]) {
		t.Errorf("--lock wrote %d bytes, %v", len(data), err)
	}
}

func TestPlanErrors(t *testing.T) {
	manifest, caFile := planFixture(t, "[curl, nope]")
	out, err := run(t, "plan", "-f", manifest, "--ca-file", caFile, "--cache-dir", t.TempDir())
	if err == nil || err.Error() != "target internal (amd64): no package nope in the target's repositories" {
		t.Errorf("error = %v", err)
	}
	// No lock when a target fails.
	if !strings.Contains(out, "packtrix.lock wasn't written: it is only written when every target is planned") {
		t.Errorf("output = %q", out)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(manifest), "packtrix.lock")); !os.IsNotExist(err) {
		t.Errorf("a lock was written: %v", err)
	}
	// Without the CA, the archive's certificate isn't trusted.
	if _, err := run(t, "plan", "-f", manifest, "--cache-dir", t.TempDir()); err == nil || !strings.Contains(err.Error(), "pass its certificate with --ca-file") {
		t.Errorf("error = %v", err)
	}
	if _, err := run(t, "plan", "-f", manifest, "-o", "yaml", "--cache-dir", t.TempDir()); err == nil || err.Error() != `unsupported output format "yaml" (supported: json)` {
		t.Errorf("error = %v", err)
	}
}

func TestSize(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 1536 * 1024: "1.5 MiB", 3 << 40: "3.0 TiB"} {
		if got := size(n); got != want {
			t.Errorf("size(%d) = %q, want %q", n, got, want)
		}
	}
}
