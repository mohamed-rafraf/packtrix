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

package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/internal/expand"
	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/repo/apt/apttest"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/validation"
)

// pkg is a paragraph of a Packages index.
func pkg(name, version, fields string) string {
	return fmt.Sprintf("Package: %s\nVersion: %s\nArchitecture: amd64\nFilename: pool/main/%s_%s_amd64.deb\nSize: %d\nSHA256: %064x\n%s\n",
		name, version, name, version, 1000*len(name), len(name+version), fields)
}

const releaseFields = `Origin: Debian
Label: Debian
Suite: test
Codename: test
Architectures: amd64
Components: main contrib
`

// env is a test distro, test-1, whose repositories a local HTTPS archive
// serves: the release suite and its updates.
type env struct {
	archive *apttest.Archive
	catalog *catalog.Catalog
	client  *fetch.Client
	dir     string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	signer, _ := apttest.Key(t)
	a := apttest.New(t, signer, true)
	a.Publish(apttest.Suite{
		Dir: "debian/dists/test", Fields: releaseFields, Compressions: []string{".xz"},
		Indexes: map[string]string{
			"main/binary-amd64/Packages": pkg("libc6", "1.0", "Priority: required\n") +
				pkg("dpkg", "1.0", "Essential: yes\nPriority: required\nDepends: libc6\n") +
				pkg("curl", "8.0", "Depends: libc6, libcurl4 (= 8.0)\n") +
				pkg("libcurl4", "8.0", "Depends: libc6\n"),
			"contrib/binary-amd64/Packages": pkg("extra-tool", "1.0", ""),
		},
	})
	a.Publish(apttest.Suite{
		Dir: "debian/dists/test-updates", Compressions: []string{".xz"},
		Fields: strings.NewReplacer("Suite: test", "Suite: test-updates", "Codename: test", "Codename: test-updates").Replace(releaseFields),
		Indexes: map[string]string{
			"main/binary-amd64/Packages": pkg("libc6", "1.1", "Priority: required\n") +
				pkg("curl", "8.1", "Depends: libc6 (>= 1.0), libcurl4 (= 8.1)\n") +
				pkg("libcurl4", "8.1", "Depends: libc6 (>= 1.1), libssl3 (>= 3.0)\n") +
				pkg("libssl3", "3.0", "") +
				pkg("broken", "1.0", "Depends: missing-lib (>= 2)\n"),
			"contrib/binary-amd64/Packages": "",
		},
	})

	public, err := signer.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	c, err := catalog.Load(fstest.MapFS{
		"catalog.yaml":  {Data: []byte("version: test\n")},
		"keys/test.asc": {Data: public},
		"distros/test-1.yaml": {Data: []byte(`
name: test-1
family: debian
format: apt
release: { version: "1", codename: test }
architectures: [amd64]
components: [{ name: main, default: true }, { name: contrib }]
repositories:
  - { name: release, url: ` + a.URL() + `/debian/, suite: test, keys: [test] }
  - { name: updates, url: ` + a.URL() + `/debian/, suite: test-updates, keys: [test] }
baseline: { repository: release, component: main }
keys: { test: ` + signer.Fingerprint() + ` }
`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := fetch.New(fetch.Options{CAFile: a.CAFile(), Retries: -1})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.asc"), public, 0o600); err != nil {
		t.Fatal(err)
	}
	return &env{archive: a, catalog: c, client: client, dir: dir}
}

func (e *env) run(t *testing.T, spec string) (*Plan, error) {
	t.Helper()
	m, err := manifest.Parse([]byte("apiVersion: packtrix.io/v1alpha1\nkind: OfflineRepository\nmetadata:\n  name: test\nspec:\n" +
		strings.ReplaceAll(spec, "ARCHIVE", e.archive.URL())))
	if err != nil {
		t.Fatal(err)
	}
	var read []string
	p, err := Run(t.Context(), m, e.catalog, e.client, Options{Dir: e.dir, Reading: func(target, arch string, r expand.Repository) {
		read = append(read, target+"/"+arch+"/"+r.Name)
	}})
	if p != nil && len(p.Targets) > 0 && len(read) == 0 {
		t.Error("Reading wasn't called")
	}
	return p, err
}

type summary struct{ name, version, repository, why string }

func summarize(t Target) []summary {
	var out []summary
	for _, p := range t.Packages {
		out = append(out, summary{p.Name, p.Version, p.Repository, strings.Join(p.Why, " ← ")})
	}
	return out
}

func TestRun(t *testing.T) {
	e := newEnv(t)
	p, err := e.run(t, `
  targets:
    - distro: test-1
  packages: [curl]
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Targets) != 1 {
		t.Fatalf("plan = %+v", p)
	}
	target := p.Targets[0]
	if target.Name != "test-1" || target.Architecture != "amd64" || target.Distribution != "Debian test" {
		t.Errorf("target = %+v", target)
	}
	// The baseline has libc6 1.0, which libcurl4 8.1 finds too old.
	want := []summary{
		{"curl", "8.1", "updates", ""},
		{"libcurl4", "8.1", "updates", "curl"},
		{"libc6", "1.1", "updates", "libcurl4 ← curl"},
		{"libssl3", "3.0", "updates", "libcurl4 ← curl"},
	}
	if got := summarize(target); !reflect.DeepEqual(got, want) {
		t.Errorf("packages = %+v, want %+v", got, want)
	}
	curl := target.Packages[0]
	if curl.URL != e.archive.URL()+"/debian/pool/main/curl_8.1_amd64.deb" || curl.Size != 4000 || len(curl.SHA256) != 64 || curl.Architecture != "amd64" {
		t.Errorf("curl = %+v", curl)
	}
	if target.Size() != 4000+8000+5000+7000 {
		t.Errorf("Size = %d", target.Size())
	}
	if len(target.Repositories) != 2 || target.Repositories[1].Name != "updates" || target.Repositories[1].Packages != 5 || target.Repositories[1].Signer == "" {
		t.Errorf("repositories = %+v", target.Repositories)
	}

	l := p.Lock("test", "sha256:"+strings.Repeat("0", 64), "test")
	if err := validation.ValidateLock(l); err != nil {
		t.Fatalf("the lock isn't valid: %v", err)
	}
	lt := l.Spec.Targets[0]
	if l.Name != "test" || lt.Format != "apt" || lt.Distribution != "Debian test" || !strings.HasPrefix(lt.Repositories[0].Release, "sha256:") ||
		!reflect.DeepEqual(lt.Repositories[0].Components, []string{"main"}) || !reflect.DeepEqual(lt.Packages[3].Why, []string{"libcurl4", "curl"}) || lt.Packages[0].URL != curl.URL {
		t.Errorf("lock = %+v", l)
	}
}

func TestSettings(t *testing.T) {
	e := newEnv(t)
	p, err := e.run(t, `
  targets:
    - distro:
        name: test-1
        components: [main, contrib]
  packages:
    - name: curl
      version: "8.0"
    - extra-tool
  resolve:
    baseline: none
    exclude: [libc6]
`)
	if err != nil {
		t.Fatal(err)
	}
	target := p.Targets[0]
	want := []summary{
		{"curl", "8.0", "release", ""},
		{"extra-tool", "1.0", "release", ""},
		{"libcurl4", "8.0", "release", "curl"},
	}
	if got := summarize(target); !reflect.DeepEqual(got, want) {
		t.Errorf("packages = %+v, want %+v", got, want)
	}
	if want := []Exclusion{{Name: "libc6", NeededBy: []string{"curl", "libcurl4"}}}; !reflect.DeepEqual(target.Excluded, want) {
		t.Errorf("excluded = %+v, want %+v", target.Excluded, want)
	}
}

func TestRunFailures(t *testing.T) {
	e := newEnv(t)
	p, err := e.run(t, `
  targets:
    - distro: test-1
    - name: unresolvable
      distro: test-1
    - name: from-scratch
      repos:
        - name: release
          type: apt
          url: ARCHIVE/debian/
          suite: test
          components: [main]
          gpgKey: test.asc
    - name: el
      repos:
        - name: baseos
          type: rpm
          url: ARCHIVE/el/
          gpgKey: test.asc
  packages:
    - name: curl
      only: [test-1, from-scratch]
    - name: nope
      only: [test-1]
    - name: broken
      only: [unresolvable]
`)
	for _, want := range []string{
		"target test-1 (amd64): no package nope in the target's repositories",
		"target unresolvable (amd64): broken=1.0 needs missing-lib (>= 2), which no package provides",
		`target from-scratch (amd64): baseline "minimal" needs a built-in distro`,
		"target el: RPM targets aren't supported yet (planned for v0.2.0)",
	} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want it to contain %q", err, want)
		}
	}
	if p == nil || len(p.Targets) != 0 {
		t.Errorf("plan = %+v, want no target", p)
	}
}

func TestMixedReleases(t *testing.T) {
	e := newEnv(t)
	const spec = `
  targets:
    - name: mixed
      repos:
        - name: test
          type: apt
          url: ARCHIVE/debian/
          suite: test
          components: [main]
          gpgKey: test.asc
        - name: other
          type: apt
          url: ARCHIVE/other/
          suite: other
          components: [main]
          gpgKey: test.asc
    - distro: test-1
  packages: [curl]
  resolve:
    baseline: none
`
	// A repository that can't be read fails its target.
	if _, err := e.run(t, spec); err == nil || !strings.Contains(err.Error(), "target mixed (amd64): repository other") {
		t.Fatalf("error = %v, want the missing repository", err)
	}

	e.archive.Publish(apttest.Suite{
		Dir: "other/dists/other", Compressions: []string{".xz"},
		Fields:  strings.NewReplacer("Suite: test", "Suite: other", "Codename: test", "Codename: other").Replace(releaseFields),
		Indexes: map[string]string{"main/binary-amd64/Packages": pkg("curl", "9.0", "")},
	})
	p, err := e.run(t, spec)
	if want := "target mixed (amd64): repositories test and other are for different releases: Debian test and Debian other; a target can't mix releases"; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
	// The other target is still planned.
	if p == nil || len(p.Targets) != 1 || p.Targets[0].Name != "test-1" {
		t.Errorf("plan = %+v, want test-1 planned", p)
	}
}

func TestExpandErrors(t *testing.T) {
	e := newEnv(t)
	_, err := e.run(t, `
  targets:
    - distro: test-2
  packages: [curl]
`)
	if want := `spec.targets[0].distro.name: unknown distro "test-2"; did you mean "test-1"?`; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}
