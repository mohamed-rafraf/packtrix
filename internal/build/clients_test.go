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

package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/packtrix/packtrix/internal/pgp"
	"github.com/packtrix/packtrix/internal/repo/apt/apttest"
)

var clientTargets = []Target{
	{Name: "ubuntu-24.04", Codename: "noble", Architectures: []string{"amd64", "arm64"}},
	{Name: "debian-12", Codename: "bookworm", Architectures: []string{"amd64"}},
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestClients(t *testing.T) {
	signer, _ := apttest.Key(t)
	dir := t.TempDir()
	if err := Clients(dir, clientTargets, signer, "http://repo.internal/"); err != nil {
		t.Fatal(err)
	}

	want := `# Packtrix repository of ubuntu-24.04 (amd64, arm64).
# Copy this file to /etc/apt/sources.list.d/packtrix.sources, and keys/packtrix-repo.gpg
# to /etc/apt/keyrings/packtrix-repo.gpg.
Types: deb
URIs: http://repo.internal/apt/ubuntu-24.04/
Suites: noble
Components: main
Signed-By: /etc/apt/keyrings/packtrix-repo.gpg
`
	if got := readFile(t, dir, "clients/ubuntu-24.04.sources"); got != want {
		t.Errorf("ubuntu-24.04.sources =\n%s\nwant\n%s", got, want)
	}
	if got := readFile(t, dir, "clients/debian-12.sources"); !strings.Contains(got, "URIs: http://repo.internal/apt/debian-12/\nSuites: bookworm\n") {
		t.Errorf("debian-12.sources =\n%s", got)
	}

	// Both keys are the signer's.
	for _, name := range []string{"keys/packtrix-repo.asc", "keys/packtrix-repo.gpg"} {
		k, err := pgp.NewKeyring([]byte(readFile(t, dir, name)), signer.Fingerprint())
		if err != nil || len(k.Fingerprints()) != 1 {
			t.Errorf("%s: %v", name, err)
		}
	}
	if !strings.HasPrefix(readFile(t, dir, "keys/packtrix-repo.asc"), "-----BEGIN PGP PUBLIC KEY BLOCK-----") ||
		strings.HasPrefix(readFile(t, dir, "keys/packtrix-repo.gpg"), "-----BEGIN") {
		t.Error("the .asc key isn't armored, or the .gpg key is")
	}

	readme := readFile(t, dir, "clients/README")
	for _, want := range []string{
		"clients/<target>.sources  ->  /etc/apt/sources.list.d/packtrix.sources\n",
		"keys/packtrix-repo.gpg    ->  /etc/apt/keyrings/packtrix-repo.gpg\n",
		"ubuntu-24.04  clients/ubuntu-24.04.sources  noble     amd64, arm64\n",
		"debian-12     clients/debian-12.sources     bookworm  amd64\n",
		"signed with the key " + signer.Fingerprint(),
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README is missing %q:\n%s", want, readme)
		}
	}
	if strings.Contains(readme, Placeholder) {
		t.Errorf("README mentions the placeholder although the URL is known:\n%s", readme)
	}
}

func TestClientsWithoutBaseURL(t *testing.T) {
	signer, _ := apttest.Key(t)
	dir := t.TempDir()
	if err := Clients(dir, clientTargets, signer, ""); err != nil {
		t.Fatal(err)
	}
	sources := readFile(t, dir, "clients/ubuntu-24.04.sources")
	for _, want := range []string{"URIs: " + Placeholder + "/apt/ubuntu-24.04/\n", "packtrix sync --base-url sets it"} {
		if !strings.Contains(sources, want) {
			t.Errorf("sources is missing %q:\n%s", want, sources)
		}
	}
	if !strings.Contains(readFile(t, dir, "clients/README"), "replace it with where the\nrepositories are served") {
		t.Error("README doesn't say to replace the placeholder")
	}

	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Clients(file, clientTargets, signer, ""); err == nil {
		t.Error("writing into a file succeeded")
	}
}
