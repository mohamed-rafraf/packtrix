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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packtrix/packtrix/internal/plan"
	"github.com/packtrix/packtrix/internal/repo/apt/apttest"
)

// planFixture writes a manifest whose target reads a local HTTPS archive,
// and returns the manifest and the archive's CA file.
func planFixture(t *testing.T, packages string) (manifest, caFile string) {
	t.Helper()
	signer, _ := apttest.Key(t)
	a := apttest.New(t, signer, true)
	a.Publish(apttest.Suite{
		Dir:          "debian/dists/test",
		Fields:       "Origin: Debian\nSuite: test\nCodename: test\n",
		Compressions: []string{".gz"},
		Indexes: map[string]string{"main/binary-amd64/Packages": `Package: curl
Version: 8.1
Architecture: amd64
Depends: libcurl4
Filename: pool/main/curl_8.1_amd64.deb
Size: 230000
SHA256: 0000000000000000000000000000000000000000000000000000000000000001

Package: libcurl4
Version: 8.1
Architecture: amd64
Filename: pool/main/libcurl4_8.1_amd64.deb
Size: 1500000
SHA256: 0000000000000000000000000000000000000000000000000000000000000002
`},
	})
	dir := t.TempDir()
	public, err := signer.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "archive.asc"), public, 0o600); err != nil {
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
`
	if err := os.WriteFile(manifest, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return manifest, a.CAFile()
}

func TestPlan(t *testing.T) {
	manifest, caFile := planFixture(t, "[curl]")
	out, err := run(t, "plan", "-f", manifest, "--ca-file", caFile)
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
	cmd.SetArgs([]string{"plan", "-f", manifest, "--ca-file", caFile, "-o", "json"})
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

func TestPlanErrors(t *testing.T) {
	manifest, caFile := planFixture(t, "[curl, nope]")
	if _, err := run(t, "plan", "-f", manifest, "--ca-file", caFile); err == nil || err.Error() != "target internal (amd64): no package nope in the target's repositories" {
		t.Errorf("error = %v", err)
	}
	// Without the CA, the archive's certificate isn't trusted.
	if _, err := run(t, "plan", "-f", manifest); err == nil || !strings.Contains(err.Error(), "pass its certificate with --ca-file") {
		t.Errorf("error = %v", err)
	}
	if _, err := run(t, "plan", "-f", manifest, "-o", "yaml"); err == nil || err.Error() != `unsupported output format "yaml" (supported: json)` {
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
