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

package expand

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
)

const ubuntuKey = "F6ECB3762474EDA9D21B7022871920D1991BC93C"

// keyFile is a key that manifest repositories and test catalogs can use.
func keyFile(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../catalog/keys/ubuntu-archive-2018.asc")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// expandSpec expands a manifest with this spec. Its directory holds
// keys/vendor.asc.
func expandSpec(t *testing.T, c *catalog.Catalog, spec string) (*Result, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "keys"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keys", "vendor.asc"), keyFile(t), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Parse([]byte("apiVersion: packtrix.io/v1alpha1\nkind: OfflineRepository\nmetadata:\n  name: test\nspec:\n" + spec))
	if err != nil {
		t.Fatal(err)
	}
	return Expand(m, c, dir)
}

func builtin(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func mustExpand(t *testing.T, c *catalog.Catalog, spec string) *Result {
	t.Helper()
	res, err := expandSpec(t, c, spec)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// urls returns "name suite url components" for each repository.
func urls(a Architecture) []string {
	var out []string
	for _, r := range a.Repositories {
		out = append(out, strings.Join(strings.Fields(strings.Join([]string{r.Name, r.Suite, r.URL, strings.Join(r.Components, ",")}, " ")), " "))
	}
	return out
}

func TestDistroTarget(t *testing.T) {
	res := mustExpand(t, builtin(t), `
  targets:
    - distro: ubuntu-24.04
      architectures: [amd64, arm64]
  packages: [curl]
`)
	target := res.Targets[0]
	if target.Name != "ubuntu-24.04" || target.Format != "apt" || target.Distro != "ubuntu-24.04" || *target.Baseline != (catalog.Baseline{Repository: "release", Component: "main"}) {
		t.Errorf("target = %+v", target)
	}
	want := map[string][]string{
		"amd64": {
			"release noble https://archive.ubuntu.com/ubuntu/ main,universe",
			"updates noble-updates https://archive.ubuntu.com/ubuntu/ main,universe",
			"security noble-security https://security.ubuntu.com/ubuntu/ main,universe",
		},
		"arm64": {
			"release noble https://ports.ubuntu.com/ubuntu-ports/ main,universe",
			"updates noble-updates https://ports.ubuntu.com/ubuntu-ports/ main,universe",
			"security noble-security https://ports.ubuntu.com/ubuntu-ports/ main,universe",
		},
	}
	for _, a := range target.Architectures {
		if got := urls(a); !reflect.DeepEqual(got, want[a.Name]) {
			t.Errorf("%s: repositories = %q, want %q", a.Name, got, want[a.Name])
		}
		for _, r := range a.Repositories {
			if !reflect.DeepEqual(r.Keys, []string{ubuntuKey}) || r.Keyring() == nil {
				t.Errorf("%s/%s: keys = %q, want the Ubuntu archive key", a.Name, r.Name, r.Keys)
			}
		}
	}
}

func TestComponentsAndMirror(t *testing.T) {
	res := mustExpand(t, builtin(t), `
  targets:
    - distro:
        name: debian-12
        components: [main, non-free-firmware]
        mirror: https://nexus.internal/repository
    - distro:
        name: ubuntu-24.04
        mirror: https://mirror.example.org/
      architectures: [arm64]
  packages: [curl]
`)
	want := [][]string{
		{
			"release bookworm https://nexus.internal/repository/debian/ main,non-free-firmware",
			"updates bookworm-updates https://nexus.internal/repository/debian/ main,non-free-firmware",
			"security bookworm-security https://nexus.internal/repository/debian-security/ main,non-free-firmware",
		},
		{
			"release noble https://mirror.example.org/ubuntu-ports/ main,universe",
			"updates noble-updates https://mirror.example.org/ubuntu-ports/ main,universe",
			"security noble-security https://mirror.example.org/ubuntu-ports/ main,universe",
		},
	}
	for i, target := range res.Targets {
		if got := urls(target.Architectures[0]); !reflect.DeepEqual(got, want[i]) {
			t.Errorf("%s: repositories = %q, want %q", target.Name, got, want[i])
		}
	}
}

func TestPackages(t *testing.T) {
	res := mustExpand(t, builtin(t), `
  targets:
    - name: ubuntu-gpu
      distro: ubuntu-24.04
      extraRepos:
        - name: vendor
          type: apt
          url: https://vendor.example.org/{arch}/
          suite: stable
          components: [main]
          gpgKey: keys/vendor.asc
    - distro: debian-12
    - name: oracle-9
      repos:
        - name: baseos
          type: rpm
          url: https://yum.oracle.com/repo/OracleLinux/OL9/baseos/latest/{arch}/
          gpgKey: keys/vendor.asc
  packages:
    - curl
    - name: apache
      deb: apache2
      rpm: httpd
    - name: kubelet
      version: "1.34.*"
    - name: nvidia-container-toolkit
      only: [ubuntu-gpu]
    - name: open-vm-tools
      only: [deb]
    - name: dnf-utils
      only: [rpm]
    - name: ceph-common
      except: [debian-12]
`)
	want := map[string][]Package{
		"ubuntu-gpu": {{Name: "curl"}, {Name: "apache2", Label: "apache"}, {Name: "kubelet", Version: "1.34.*"}, {Name: "nvidia-container-toolkit"}, {Name: "open-vm-tools"}, {Name: "ceph-common"}},
		"debian-12":  {{Name: "curl"}, {Name: "apache2", Label: "apache"}, {Name: "kubelet", Version: "1.34.*"}, {Name: "open-vm-tools"}},
		"oracle-9":   {{Name: "curl"}, {Name: "httpd", Label: "apache"}, {Name: "kubelet", Version: "1.34.*"}, {Name: "dnf-utils"}, {Name: "ceph-common"}},
	}
	for _, target := range res.Targets {
		if !reflect.DeepEqual(target.Packages, want[target.Name]) {
			t.Errorf("%s: packages = %+v, want %+v", target.Name, target.Packages, want[target.Name])
		}
	}

	// {arch} is the native name: amd64 for APT, x86_64 for RPM. A distro
	// target's own repositories come first.
	gpu := res.Targets[0].Architectures[0].Repositories
	if last := gpu[len(gpu)-1]; last.URL != "https://vendor.example.org/amd64/" || !reflect.DeepEqual(last.Keys, []string{ubuntuKey}) {
		t.Errorf("vendor repository = %+v", last)
	}
	oracle := res.Targets[2]
	if oracle.Format != "rpm" || oracle.Distro != "" || oracle.Baseline != nil {
		t.Errorf("oracle-9 = %+v, want an RPM target without a distro", oracle)
	}
	if got := urls(oracle.Architectures[0]); !reflect.DeepEqual(got, []string{"baseos https://yum.oracle.com/repo/OracleLinux/OL9/baseos/latest/x86_64/"}) {
		t.Errorf("oracle-9 repositories = %q", got)
	}
}

// rpmCatalog has an EL entry, for the RPM parts of the catalog.
func rpmCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load(fstest.MapFS{
		"catalog.yaml":                 {Data: []byte("version: test\n")},
		"keys/ubuntu-archive-2018.asc": {Data: keyFile(t)},
		"distros/rocky-9.yaml": {Data: []byte(`
name: rocky-9
family: el
format: rpm
release: { version: "9" }
architectures: [amd64]
components:
  - { name: baseos, default: true, path: BaseOS }
  - { name: appstream, default: true, path: AppStream }
  - { name: crb, path: CRB }
repositories:
  - name: os
    url: https://dl.rockylinux.org/pub/rocky/9/{component}/{arch}/os/
    keys: [ubuntu-archive-2018]
baseline: { repository: os, component: baseos }
keys:
  ubuntu-archive-2018: ` + ubuntuKey + `
`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRPMDistro(t *testing.T) {
	res := mustExpand(t, rpmCatalog(t), `
  targets:
    - distro:
        name: rocky-9
        components: [baseos, crb]
        mirror: https://nexus.internal/repository/
  packages: [curl]
`)
	want := []string{
		"os-baseos https://nexus.internal/repository/pub/rocky/9/BaseOS/x86_64/os/",
		"os-crb https://nexus.internal/repository/pub/rocky/9/CRB/x86_64/os/",
	}
	if got := urls(res.Targets[0].Architectures[0]); !reflect.DeepEqual(got, want) {
		t.Errorf("repositories = %q, want %q", got, want)
	}
}

func TestErrors(t *testing.T) {
	_, err := expandSpec(t, builtin(t), `
  targets:
    - distro: ubuntu-24.4
    - distro:
        name: ubuntu-24.04
        components: [main, contrib]
      extraRepos:
        - name: el
          type: rpm
          url: https://el.example.org/
          gpgKey: keys/vendor.asc
        - name: missing
          type: apt
          url: https://apt.example.org/
          suite: stable
          components: [main]
          gpgKey: keys/missing.asc
    - name: other
      distro: debian-12
  packages:
    - name: nginx
      repo: nginx-org
    - name: kubelet
      version: "v1.34"
`)
	want := strings.Join([]string{
		"6 problems:",
		`  - spec.targets[0].distro.name: unknown distro "ubuntu-24.4"; did you mean "ubuntu-24.04"?`,
		`  - spec.targets[1].distro.components[1]: ubuntu-24.04 has no component "contrib" (components: main, universe, restricted, multiverse)`,
		"  - spec.targets[1].extraRepos[0].type: the target's repositories are apt, not rpm",
		"  - spec.targets[1].extraRepos[1].gpgKey: open ",
		`  - spec.packages[0].repo: unknown vendor repository "nginx-org": the catalog has no vendor repositories yet`,
		`  - spec.packages[1].version: invalid version constraint "v1.34"`,
	}, "\n")
	for _, line := range strings.Split(want, "\n") {
		if err == nil || !strings.Contains(err.Error(), line) {
			t.Errorf("error = %v, want it to contain %q", err, line)
		}
	}

	_, err = expandSpec(t, rpmCatalog(t), `
  targets:
    - distro: rocky-9
      architectures: [amd64, arm64]
  packages: [curl]
`)
	if want := "spec.targets[0].architectures[1]: rocky-9 doesn't support arm64 (supported: amd64)"; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}
