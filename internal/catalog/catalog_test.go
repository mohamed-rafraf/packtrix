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

package catalog

import (
	"io"
	"io/fs"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func builtinCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func distro(t *testing.T, c *Catalog, name string) *Distro {
	t.Helper()
	d, err := c.Distro(name)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func repository(t *testing.T, d *Distro, name string) Repository {
	t.Helper()
	for _, r := range d.Repositories {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("%s has no repository %s", d.Name, name)
	return Repository{}
}

func names(distros []*Distro) []string {
	var out []string
	for _, d := range distros {
		out = append(out, d.Name)
	}
	return out
}

func TestBuiltin(t *testing.T) {
	c := builtinCatalog(t)
	if c.Version == "" {
		t.Error("the catalog has no version")
	}
	if got, want := names(c.Distros()), []string{"debian-12", "debian-13", "ubuntu-22.04", "ubuntu-24.04"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Distros = %q, want %q", got, want)
	}

	d := distro(t, c, "ubuntu-24.04")
	if got := d.DefaultComponents(); !reflect.DeepEqual(got, []string{"main", "universe"}) {
		t.Errorf("DefaultComponents = %q, want main and universe", got)
	}
	security := repository(t, d, "security")
	for arch, want := range map[string]string{"amd64": "https://security.ubuntu.com/ubuntu/", "arm64": "https://ports.ubuntu.com/ubuntu-ports/"} {
		if got := security.URLFor(arch); got != want {
			t.Errorf("security.URLFor(%s) = %s, want %s", arch, got, want)
		}
	}
}

// The catalog pins the keys that sign the real security suites, which the
// pgp package keeps as fixtures.
func TestKeysVerifySecurityFixtures(t *testing.T) {
	c := builtinCatalog(t)
	for distroName, fixture := range map[string]string{
		"ubuntu-24.04": "ubuntu-noble-security.InRelease",
		"debian-12":    "debian-bookworm-security.InRelease",
	} {
		d := distro(t, c, distroName)
		k, err := d.Keyring(repository(t, d, "security"))
		if err != nil {
			t.Fatal(err)
		}
		message, err := os.ReadFile("../pgp/testdata/" + fixture)
		if err != nil {
			t.Fatal(err)
		}
		if _, signer, err := k.VerifyClearsigned(message); err != nil {
			t.Errorf("%s: %v", distroName, err)
		} else if !slices.Contains(k.Fingerprints(), signer) {
			t.Errorf("%s: signed by %s, which isn't pinned", distroName, signer)
		}
	}
}

func TestUnknownDistro(t *testing.T) {
	c := builtinCatalog(t)
	for name, want := range map[string]string{
		"ubuntu-24.4": `unknown distro "ubuntu-24.4"; did you mean "ubuntu-24.04"?`,
		"debian12":    `unknown distro "debian12"; did you mean "debian-12"?`,
		"gentoo":      `unknown distro "gentoo" (known: debian-12, debian-13, ubuntu-22.04, ubuntu-24.04)`,
	} {
		if _, err := c.Distro(name); err == nil || err.Error() != want {
			t.Errorf("Distro(%q) = %v, want %q", name, err, want)
		}
	}
}

// testCatalog is a catalog with the built-in keys and the given entries.
func testCatalog(t *testing.T, entries map[string]string) fstest.MapFS {
	t.Helper()
	fsys := fstest.MapFS{"catalog.yaml": {Data: []byte("version: test\n")}}
	keys, err := fs.Glob(builtin, "keys/*.asc")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		data, err := fs.ReadFile(builtin, k)
		if err != nil {
			t.Fatal(err)
		}
		fsys[k] = &fstest.MapFile{Data: data}
	}
	for name, entry := range entries {
		fsys["distros/"+name+".yaml"] = &fstest.MapFile{Data: []byte(entry)}
	}
	return fsys
}

// rpmEntry is shaped like an EL entry. No RPM distribution ships yet, so it
// borrows the Ubuntu key.
const rpmEntry = `
name: rocky-VERSION
family: el
format: rpm
release:
  version: "VERSION"
architectures: [amd64, arm64]
components:
  - name: baseos
    default: true
    path: BaseOS
  - name: appstream
    default: true
    path: AppStream
  - name: crb
    path: CRB
repositories:
  - name: os
    url: https://dl.rockylinux.org/pub/rocky/VERSION/{component}/{arch}/os/
    keys: [ubuntu-archive-2018]
baseline:
  repository: os
  component: baseos
keys:
  ubuntu-archive-2018: F6ECB3762474EDA9D21B7022871920D1991BC93C
`

func TestRPMEntry(t *testing.T) {
	c, err := Load(testCatalog(t, map[string]string{
		"rocky-10": strings.ReplaceAll(rpmEntry, "VERSION", "10"),
		"rocky-9":  strings.ReplaceAll(rpmEntry, "VERSION", "9"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	// Releases sort as numbers.
	if got, want := names(c.Distros()), []string{"rocky-9", "rocky-10"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Distros = %q, want %q", got, want)
	}
	d := distro(t, c, "rocky-9")
	if got := d.DefaultComponents(); !reflect.DeepEqual(got, []string{"baseos", "appstream"}) {
		t.Errorf("DefaultComponents = %q", got)
	}
	if got, want := d.Repositories[0].URLFor("amd64"), "https://dl.rockylinux.org/pub/rocky/9/{component}/{arch}/os/"; got != want {
		t.Errorf("URLFor = %s, want %s", got, want)
	}
}

const aptEntry = `
name: test-1
family: debian
format: apt
release:
  version: "1"
  codename: test
architectures: [amd64]
components:
  - name: main
    default: true
repositories:
  - name: release
    url: https://deb.example.org/debian/
    suite: test
    keys: [debian-archive-12]
baseline:
  repository: release
  component: main
keys:
  debian-archive-12: B8B80B5B623EAB6AD8775C45B7C5D7D6350947F8
`

func TestLoadRejects(t *testing.T) {
	for _, tt := range []struct {
		name        string
		old, new    string
		file, want  string
		keyContents string
	}{
		{name: "file name", file: "test-2", want: `name: "test-1" doesn't match the file name`},
		{name: "family", old: "family: debian", new: "family: arch", want: `family: "arch" isn't one of ubuntu, debian, el`},
		{name: "format", old: "format: apt", new: "format: rpm", want: `format: debian distributions use apt, not "rpm"`},
		{name: "codename", old: "codename: test", new: "codename: ''", want: "release.codename: required for APT distributions"},
		{name: "version", old: `version: "1"`, new: `version: ""`, want: "release.version: required"},
		{name: "architecture", old: "[amd64]", new: "[amd64, i386]", want: `architectures[1]: unsupported or duplicate architecture "i386"`},
		{name: "no default", old: "    default: true\n", new: "", want: "components: at least one must be a default"},
		{name: "component path", old: "    default: true\n", new: "    default: true\n    path: Main\n", want: "components[0].path: only RPM components have a path"},
		{name: "suite", old: "    suite: test\n", new: "", want: "repositories[0].suite: required for APT repositories"},
		{name: "http", old: "https://deb", new: "http://deb", want: `repositories[0]: URL "http://deb.example.org/debian/" must be https://`},
		{name: "slash", old: "debian/\n", new: "debian\n", want: `repositories[0]: URL "https://deb.example.org/debian" must end with /`},
		{name: "placeholder", old: "debian/\n", new: "{arch}/\n", want: "APT repositories have no placeholders; use archURLs"},
		{name: "arch URL", old: "    suite: test\n", new: "    suite: test\n    archURLs:\n      arm64: https://ports.example.org/\n", want: `repositories[0].archURLs: "arm64" isn't an architecture of the distribution`},
		{name: "unknown key", old: "keys: [debian-archive-12]", new: "keys: [debian-archive-13]", want: `repositories[0].keys: unknown key "debian-archive-13"`},
		{name: "baseline", old: "repository: release", new: "repository: updates", want: `baseline.repository: unknown repository "updates"`},
		{name: "unknown field", old: "family: debian", new: "family: debian\nmirror: x", want: `unknown field "mirror"`},
		{name: "missing key file", old: "debian-archive-12: B8B8", new: "debian-archive-99: B8B8", want: "keys.debian-archive-99: open keys/debian-archive-99.asc"},
		{name: "wrong fingerprint", old: "debian-archive-12: B8B80B5B623EAB6AD8775C45B7C5D7D6350947F8", new: "debian-archive-12: 05AB90340C0C5E797F44A8C8254CF3B5AEC0A8F0",
			want: "keys.debian-archive-12: the keys don't include the pinned key 05AB90340C0C5E797F44A8C8254CF3B5AEC0A8F0"},
	} {
		file := cmpOr(tt.file, "test-1")
		_, err := Load(testCatalog(t, map[string]string{file: strings.Replace(aptEntry, tt.old, tt.new, 1)}))
		if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.HasPrefix(err.Error(), "distros/"+file+".yaml: ") {
			t.Errorf("%s: error = %v, want it to contain %q", tt.name, err, tt.want)
		}
	}

	fsys := testCatalog(t, nil)
	fsys["catalog.yaml"] = &fstest.MapFile{Data: []byte("version: ''\n")}
	if _, err := Load(fsys); err == nil || err.Error() != "catalog.yaml: version: required" {
		t.Errorf("empty version: error = %v", err)
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// TestLiveCatalog checks every repository of every entry, for every
// architecture: its InRelease exists and is signed by a pinned key.
func TestLiveCatalog(t *testing.T) {
	if testing.Short() {
		t.Skip("downloads the InRelease file of every repository")
	}
	for _, d := range builtinCatalog(t).Distros() {
		for _, r := range d.Repositories {
			k, err := d.Keyring(r)
			if err != nil {
				t.Fatal(err)
			}
			for _, arch := range d.Architectures {
				url := r.URLFor(arch) + "dists/" + r.Suite + "/InRelease"
				t.Run(d.Name+"/"+r.Name+"/"+arch, func(t *testing.T) {
					message := get(t, url)
					if _, signer, err := k.VerifyClearsigned(message); err != nil {
						t.Errorf("%s: %v", url, err)
					} else {
						t.Logf("%s: signed by %s", url, signer)
					}
				})
			}
		}
	}
}

func get(t *testing.T, url string) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
