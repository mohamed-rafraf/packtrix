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
	manifest := `apiVersion: packtrix.dev/v1alpha1
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
