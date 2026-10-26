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
	"slices"
	"strings"
	"testing"

	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/internal/expand"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/validation"
)

// The starter manifest validates, and its targets expand, for every
// distro of the catalog, alone and together.
func TestInit(t *testing.T) {
	c, err := catalog.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	names := distroNames(c)
	sets := [][]string{names}
	for _, n := range names {
		sets = append(sets, []string{n})
	}
	for _, set := range sets {
		t.Run(strings.Join(set, ","), func(t *testing.T) {
			args := []string{"init", "--name", "test"}
			for _, n := range set {
				args = append(args, "--target", n)
			}
			out, err := run(t, args...)
			if err != nil {
				t.Fatal(err)
			}
			m, err := manifest.Parse([]byte(out))
			if err == nil {
				err = validation.ValidateOfflineRepository(m)
			}
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			e, err := expand.Expand(m, c, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, target := range e.Targets {
				got = append(got, target.Name)
			}
			if !slices.Equal(got, set) || m.Name != "test" || len(m.Spec.Packages) != 1 || m.Spec.Packages[0].Name != "curl" {
				t.Errorf("targets %q, name %q, packages %v", got, m.Name, m.Spec.Packages)
			}
		})
	}
}

func TestInitPackages(t *testing.T) {
	out, err := run(t, "init", "--name", "test", "--target", "ubuntu-24.04,debian-12", "--package", "curl,jq", "--package", "nginx")
	if err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Parse([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	var pkgs []string
	for _, p := range m.Spec.Packages {
		pkgs = append(pkgs, p.Name)
	}
	if !slices.Equal(pkgs, []string{"curl", "jq", "nginx"}) || len(m.Spec.Targets) != 2 {
		t.Errorf("packages %q, %d targets", pkgs, len(m.Spec.Targets))
	}
	if strings.Contains(out, "an example") {
		t.Error("the example comment stays with listed packages")
	}
}

func TestInitFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Edge Site_2")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "packtrix.yaml")
	out, err := run(t, "init", "--target", "debian-13", "-f", file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Wrote "+file) {
		t.Errorf("output = %q", out)
	}
	m, err := manifest.LoadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "edge-site-2" {
		t.Errorf("name = %q, want the directory's", m.Name)
	}

	// An existing file stays as it is.
	if err := os.WriteFile(file, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(t, "init", "--target", "debian-13", "-f", file); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %v", err)
	}
	if data, _ := os.ReadFile(file); string(data) != "mine" {
		t.Errorf("the file was overwritten: %q", data)
	}
}

func TestInitRefuses(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "name the distros of your machines with --target"},
		{[]string{"--target", "ubuntu-24.4"}, `did you mean "ubuntu-24.04"`},
		{[]string{"--target", "debian-12,debian-12"}, `duplicate target name "debian-12"`},
		{[]string{"--target", "debian-12", "--package", "cu rl"}, `spec.packages[0]`},
		{[]string{"--target", "debian-12", "--name", "My Repos"}, "metadata.name"},
	} {
		_, err := run(t, append([]string{"init"}, tc.args...)...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("init %q: error = %v, want %q", tc.args, err, tc.want)
		}
	}
}

func TestDefaultName(t *testing.T) {
	for dir, want := range map[string]string{
		"/srv/edge-site":                  "edge-site",
		"/srv/Edge Site_2":                "edge-site-2",
		"/srv/--":                         "packtrix",
		"/":                               "packtrix",
		"/srv/" + strings.Repeat("a", 64): "packtrix",
	} {
		if got := defaultName(dir); got != want {
			t.Errorf("defaultName(%q) = %q, want %q", dir, got, want)
		}
	}
}
