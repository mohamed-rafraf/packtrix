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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestValidate(t *testing.T) {
	out, err := run(t, "validate", "-f", "../../examples/k8s-node-prereqs.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if want := "is valid: 3 target(s), 25 package(s)"; !strings.Contains(out, want) {
		t.Errorf("output = %q, want it to contain %q", out, want)
	}
}

func TestValidateOutputYAML(t *testing.T) {
	out, err := run(t, "validate", "-f", "../../examples/k8s-node-prereqs.yaml", "-o", "yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"baseline: minimal", "keepSnapshots: 3", "- amd64"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing default %q", want)
		}
	}
}

func TestValidateInvalidManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "packtrix.yaml")
	manifest := `apiVersion: packtrix.io/v1alpha1
kind: OfflineRepository
metadata: {name: test}
spec:
  targets: [{distro: ubuntu-24.04, architectures: [x86_64]}]
  packages: [curl]
`
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := run(t, "validate", "-f", path)
	if err == nil || !strings.Contains(err.Error(), `unsupported architecture "x86_64"`) {
		t.Errorf("error = %v, want an unsupported architecture error", err)
	}
}

func TestValidateMissingFile(t *testing.T) {
	_, err := run(t, "validate", "-f", filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("want an error for a missing manifest")
	}
}

func TestValidateUnsupportedOutput(t *testing.T) {
	_, err := run(t, "validate", "-f", "../../examples/k8s-node-prereqs.yaml", "-o", "json")
	if err == nil || !strings.Contains(err.Error(), "unsupported output format") {
		t.Errorf("error = %v, want unsupported output format", err)
	}
}

func TestVersionShort(t *testing.T) {
	out, err := run(t, "version", "--short")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) == "" || strings.Contains(out, "\n\n") {
		t.Errorf("version --short = %q, want a single version line", out)
	}
}

func TestCatalogList(t *testing.T) {
	out, err := run(t, "catalog", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ubuntu-24.04  24.04 (noble)  apt", "debian-12     12 (bookworm)  apt", "Catalog version "} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestCatalogShow(t *testing.T) {
	out, err := run(t, "catalog", "show", "ubuntu-24.04")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"security    noble-security  ubuntu-archive-2018  amd64         https://security.ubuntu.com/ubuntu/\n",
		"                                                 arm64         https://ports.ubuntu.com/ubuntu-ports/\n",
		"ubuntu-archive-2018  F6EC B376 2474 EDA9 D21B  7022 8719 20D1 991B C93C",
		"Components:     main, universe; optional: restricted, multiverse",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}

	_, err = run(t, "catalog", "show", "ubuntu-24.4")
	if want := `unknown distro "ubuntu-24.4"; did you mean "ubuntu-24.04"?`; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func TestRender(t *testing.T) {
	out, err := run(t, "render", "-f", "../../examples/k8s-node-prereqs.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"  distro: ubuntu-22.04\n  format: apt\n  name: ubuntu-22.04\n",
		"suite: jammy-security\n      url: https://security.ubuntu.com/ubuntu/\n",
		"suite: bookworm-security\n      url: https://security.debian.org/debian-security/\n",
		"- F6ECB3762474EDA9D21B7022871920D1991BC93C\n",
		"- 05AB90340C0C5E797F44A8C8254CF3B5AEC0A8F0\n",
		"- name: glusterfs-client\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q", want)
		}
	}
}

func TestRenderUnknownDistro(t *testing.T) {
	file := filepath.Join(t.TempDir(), "packtrix.yaml")
	manifest := "apiVersion: packtrix.io/v1alpha1\nkind: OfflineRepository\nmetadata:\n  name: test\nspec:\n  targets:\n    - distro: ubuntu-24.4\n  packages: [curl]\n"
	if err := os.WriteFile(file, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := run(t, "render", "-f", file)
	if want := `spec.targets[0].distro.name: unknown distro "ubuntu-24.4"; did you mean "ubuntu-24.04"?`; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}
