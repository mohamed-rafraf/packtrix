package v1alpha1

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const header = `apiVersion: packtrix.dev/v1alpha1
kind: OfflineRepository
metadata:
  name: test
`

// The examples in the repository are documentation; they must stay valid.
func TestExamplesAreValid(t *testing.T) {
	files, err := filepath.Glob("../../../../examples/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no examples found")
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			if _, err := LoadFile(f); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestLoadFileDocsMinimalManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "packtrix.yaml")
	manifest := header + `spec:
  targets:
    - distro: ubuntu-24.04
    - distro: rocky-9
  packages: [curl, jq, nfs-common]
`
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(r.Spec.Targets); got != 2 {
		t.Errorf("targets = %d, want 2", got)
	}
}

func TestParseShortAndLongForms(t *testing.T) {
	r, err := Parse([]byte(header + `spec:
  targets:
    - distro: ubuntu-24.04
    - distro:
        name: rocky-9
        components: [baseos, appstream, crb]
  packages:
    - curl
    - name: kubelet
      version: "1.34.*"
    - name: apache
      deb: apache2
      rpm: httpd
  updates:
    minAge: 72h
  export:
    splitSize: 4GiB
`))
	if err != nil {
		t.Fatal(err)
	}
	s := r.Spec
	wantTargets := []Target{
		{Distro: &Distro{Name: "ubuntu-24.04"}},
		{Distro: &Distro{Name: "rocky-9", Components: []string{"baseos", "appstream", "crb"}}},
	}
	if !reflect.DeepEqual(s.Targets, wantTargets) {
		t.Errorf("targets = %+v, want %+v", s.Targets, wantTargets)
	}
	wantPackages := []Package{
		{Name: "curl"},
		{Name: "kubelet", Version: "1.34.*"},
		{Name: "apache", Deb: "apache2", RPM: "httpd"},
	}
	if !reflect.DeepEqual(s.Packages, wantPackages) {
		t.Errorf("packages = %+v, want %+v", s.Packages, wantPackages)
	}
	if s.Updates.MinAge.Duration != 72*time.Hour {
		t.Errorf("minAge = %v, want 72h", s.Updates.MinAge)
	}
	if s.Export.SplitSize != 4*GiB {
		t.Errorf("splitSize = %d, want %d", s.Export.SplitSize, 4*GiB)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     string
	}{
		{
			name:     "empty",
			manifest: "  \n",
			want:     "the manifest is empty",
		},
		{
			name:     "wrong apiVersion",
			manifest: "apiVersion: packtrix.dev/v9\nkind: OfflineRepository\n",
			want:     `unsupported manifest (apiVersion "packtrix.dev/v9"`,
		},
		{
			name:     "unknown top-level field",
			manifest: header + "spec:\n  targets: [{distro: ubuntu-24.04}]\n  packages: [curl]\n  publsh: {}\n",
			want:     `unknown field "publsh"`,
		},
		{
			name:     "unknown field in a target",
			manifest: header + "spec:\n  targets: [{distro: ubuntu-24.04, architecture: [arm64]}]\n  packages: [curl]\n",
			want:     `unknown field "architecture"`,
		},
		{
			// Package has a custom unmarshaler; strictness must still apply.
			name:     "unknown field in a package object",
			manifest: header + "spec:\n  targets: [{distro: ubuntu-24.04}]\n  packages:\n    - name: kubelet\n      versoin: \"1.34.*\"\n",
			want:     `unknown field "versoin"`,
		},
		{
			name:     "unknown field in a distro object",
			manifest: header + "spec:\n  targets: [{distro: {name: rocky-9, component: [crb]}}]\n  packages: [curl]\n",
			want:     `unknown field "component"`,
		},
		{
			name:     "unquoted numeric version",
			manifest: header + "spec:\n  targets: [{distro: ubuntu-24.04}]\n  packages:\n    - name: kubelet\n      version: 1.30\n",
			want:     "must be a quoted string",
		},
		{
			name:     "package that is neither a name nor an object",
			manifest: header + "spec:\n  targets: [{distro: ubuntu-24.04}]\n  packages: [[curl]]\n",
			want:     "a package must be a name or an object",
		},
		{
			name:     "duplicate key",
			manifest: header + "spec:\n  targets: [{distro: ubuntu-24.04}]\n  packages: [curl]\n  packages: [jq]\n",
			want:     `key "packages" already set`,
		},
		{
			name:     "invalid duration",
			manifest: header + "spec:\n  targets: [{distro: ubuntu-24.04}]\n  packages: [curl]\n  updates: {minAge: 3d}\n",
			want:     `invalid duration "3d"`,
		},
		{
			name:     "invalid size",
			manifest: header + "spec:\n  targets: [{distro: ubuntu-24.04}]\n  packages: [curl]\n  export: {splitSize: 4GB2}\n",
			want:     `invalid size "4GB2"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.manifest))
			if err == nil {
				t.Fatalf("Parse succeeded, want error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestDefault(t *testing.T) {
	r, err := Parse([]byte(header + `spec:
  targets:
    - distro: ubuntu-24.04
    - name: oracle-9
      repos: [{name: ol9, type: rpm, url: "https://yum.oracle.com/{arch}/", gpgKey: k}]
  packages: [curl]
`))
	if err != nil {
		t.Fatal(err)
	}
	r.Default()
	s := r.Spec

	if got := s.Targets[0].Name; got != "ubuntu-24.04" {
		t.Errorf("target name = %q, want the distro name", got)
	}
	for i, tg := range s.Targets {
		if !reflect.DeepEqual(tg.Architectures, []string{ArchAMD64}) {
			t.Errorf("targets[%d].architectures = %v, want [amd64]", i, tg.Architectures)
		}
	}
	checks := []struct {
		field     string
		got, want any
	}{
		{"autoRepos.enabled", s.AutoRepos.IsEnabled(), true},
		{"resolve.baseline", s.Resolve.Baseline, BaselineMinimal},
		{"resolve.alternatives", s.Resolve.Alternatives, AlternativesFirst},
		{"resolve.recommends", s.Resolve.Recommends, false},
		{"updates.accept", s.Updates.Accept, AcceptAll},
		{"updates.publish", s.Updates.Publish, PublishApproval},
		{"publish.path", s.Publish.Path, DefaultPublishPath},
		{"publish.keepSnapshots", *s.Publish.KeepSnapshots, DefaultKeepSnapshots},
		{"export.format", s.Export.Format, ExportFormatTarZst},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.field, c.got, c.want)
		}
	}
	if err := r.Validate(); err != nil {
		t.Errorf("defaulted manifest is invalid: %v", err)
	}
}

func TestDefaultKeepsExplicitValues(t *testing.T) {
	r, err := Parse([]byte(header + `spec:
  targets: [{name: gpu, distro: ubuntu-24.04, architectures: [arm64]}]
  packages: [curl]
  autoRepos: {enabled: false}
  publish: {keepSnapshots: 0}
`))
	if err != nil {
		t.Fatal(err)
	}
	r.Default()
	s := r.Spec
	if s.Targets[0].Name != "gpu" || s.Targets[0].Architectures[0] != ArchARM64 {
		t.Errorf("target = %+v, explicit name and architectures were overwritten", s.Targets[0])
	}
	if s.AutoRepos.IsEnabled() {
		t.Error("autoRepos.enabled = true, want the explicit false")
	}
	if *s.Publish.KeepSnapshots != 0 {
		t.Errorf("keepSnapshots = %d, want the explicit 0", *s.Publish.KeepSnapshots)
	}
}
