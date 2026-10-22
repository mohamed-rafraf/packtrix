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
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/utils/dump"

	"github.com/packtrix/packtrix/pkg/apis/packtrix"
)

const header = `apiVersion: packtrix.io/v1alpha1
kind: OfflineRepository
metadata:
  name: test
`

func examples(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../../../../examples/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no examples found")
	}
	return files
}

// The examples in the repository are documentation; they must stay valid.
func TestExamplesAreValid(t *testing.T) {
	for _, f := range examples(t) {
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

// Parse returns the internal API with the version's defaults applied.
func TestParseDefaultsAndConverts(t *testing.T) {
	r, err := Parse([]byte(header + `spec:
  targets: [{distro: ubuntu-24.04}]
  packages: [curl, {name: kubelet, version: "1.34.*"}]
  updates: {minAge: 72h}
  export: {splitSize: 4GiB}
`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "test" {
		t.Errorf("name = %q, want test", r.Name)
	}
	s := r.Spec
	wantTarget := packtrix.Target{
		Name:          "ubuntu-24.04",
		Distro:        &packtrix.Distro{Name: "ubuntu-24.04"},
		Architectures: []string{packtrix.ArchAMD64},
	}
	if !reflect.DeepEqual(s.Targets, []packtrix.Target{wantTarget}) {
		t.Errorf("targets = %+v, want [%+v]", s.Targets, wantTarget)
	}
	wantPackages := []packtrix.Package{{Name: "curl"}, {Name: "kubelet", Version: "1.34.*"}}
	if !reflect.DeepEqual(s.Packages, wantPackages) {
		t.Errorf("packages = %+v, want %+v", s.Packages, wantPackages)
	}
	if s.Updates.MinAge != 72*time.Hour {
		t.Errorf("minAge = %v, want 72h", s.Updates.MinAge)
	}
	if s.Export.SplitSize != 4*packtrix.GiB {
		t.Errorf("splitSize = %v, want 4GiB", s.Export.SplitSize)
	}
	if !s.AutoRepos.IsEnabled() || s.Publish.Path != "./repos" {
		t.Errorf("settings = %+v, want defaults", s)
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
			manifest: "apiVersion: packtrix.io/v9\nkind: OfflineRepository\n",
			want:     `unsupported manifest (apiVersion "packtrix.io/v9", kind "OfflineRepository"): expected apiVersion "packtrix.io/v1alpha1"`,
		},
		{
			name:     "former API group",
			manifest: "apiVersion: packtrix.dev/v1alpha1\nkind: OfflineRepository\n",
			want:     `unsupported manifest (apiVersion "packtrix.dev/v1alpha1"`,
		},
		{
			name:     "wrong kind",
			manifest: "apiVersion: packtrix.io/v1alpha1\nkind: Repository\n",
			want:     `unsupported manifest (apiVersion "packtrix.io/v1alpha1", kind "Repository")`,
		},
		{
			// metav1 registers kinds such as WatchEvent in every API version.
			name:     "other kind registered in the group",
			manifest: "apiVersion: packtrix.io/v1alpha1\nkind: WatchEvent\n",
			want:     `unsupported manifest (apiVersion "packtrix.io/v1alpha1", kind "WatchEvent")`,
		},
		{
			name:     "internal version",
			manifest: "apiVersion: packtrix.io/__internal\nkind: OfflineRepository\n",
			want:     `unsupported manifest (apiVersion "packtrix.io/__internal"`,
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
			name:     "unknown field in metadata",
			manifest: "apiVersion: packtrix.io/v1alpha1\nkind: OfflineRepository\nmetadata: {name: test, nmae: x}\n",
			want:     `unknown field "nmae"`,
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

// Marshal writes the preferred version, and the result parses back to the
// same manifest. Empty lists may come back as nil, which means the same.
func TestMarshalRoundTrip(t *testing.T) {
	for _, f := range examples(t) {
		t.Run(filepath.Base(f), func(t *testing.T) {
			r, err := LoadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			out, err := Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(out), "apiVersion: packtrix.io/v1alpha1\nkind: OfflineRepository\n") {
				t.Errorf("output doesn't start with the preferred apiVersion and kind:\n%s", out)
			}
			again, err := Parse(out)
			if err != nil {
				t.Fatalf("printed manifest doesn't parse: %v\n%s", err, out)
			}
			if !apiequality.Semantic.DeepEqual(again, r) {
				t.Errorf("after a round trip:\n%s\nwant:\n%s", dump.Pretty(again), dump.Pretty(r))
			}
		})
	}
}
