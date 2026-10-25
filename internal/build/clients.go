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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/packtrix/packtrix/internal/pgp"
)

const (
	// KeyName is the name of the repositories' public key under keys/.
	KeyName = "packtrix-repo"
	// keyPath is where machines keep the key, as apt does for keys of
	// repositories that aren't the distro's, since apt 2.4.
	keyPath = "/etc/apt/keyrings/" + KeyName + ".gpg"
	// Placeholder stands for the URL where the repositories are served,
	// when it isn't known. apt fails on it, so it can't go unnoticed.
	Placeholder = "http://packtrix-repository.invalid"
)

// Clients writes what the machines need to use the repositories, into dir:
//
//	keys/packtrix-repo.asc        the public key, armored
//	keys/packtrix-repo.gpg        the same key, binary, for Signed-By
//	clients/<target>.sources      the source entry of each target
//	clients/README                which files to copy where
//
// baseURL is where dir is served, such as http://repo.internal; without it,
// the source entries use Placeholder.
func Clients(dir string, targets []Target, signer *pgp.Signer, baseURL string) error {
	armored, err := signer.PublicKey()
	if err != nil {
		return err
	}
	binary, err := signer.PublicKeyring()
	if err != nil {
		return err
	}
	files := map[string][]byte{
		"keys/" + KeyName + ".asc": armored,
		"keys/" + KeyName + ".gpg": binary,
		"clients/README":           []byte(readme(targets, signer.Fingerprint(), baseURL)),
	}
	base := strings.TrimSuffix(baseURL, "/")
	if base == "" {
		base = Placeholder
	}
	for _, t := range targets {
		files["clients/"+t.Name+".sources"] = []byte(sources(t, base, baseURL == ""))
	}
	for name, data := range files {
		if err := writeFile(filepath.Join(dir, filepath.FromSlash(name)), data); err != nil {
			return err
		}
	}
	return nil
}

// sources returns a target's deb822 source entry.
func sources(t Target, base string, placeholder bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Packtrix repository of %s (%s).\n", t.Name, strings.Join(t.Architectures, ", "))
	fmt.Fprintf(&b, "# Copy this file to /etc/apt/sources.list.d/packtrix.sources, and keys/%s.gpg\n# to %s.\n", KeyName, keyPath)
	if placeholder {
		fmt.Fprintf(&b, "# Replace %s with the URL of the repositories: packtrix sync --base-url sets it.\n", Placeholder)
	}
	fmt.Fprintf(&b, "Types: deb\nURIs: %s/apt/%s/\nSuites: %s\nComponents: main\nSigned-By: %s\n", base, t.Name, t.Codename, keyPath)
	return b.String()
}

// readme returns clients/README.
func readme(targets []Target, fingerprint, baseURL string) string {
	var b strings.Builder
	b.WriteString(`Using the Packtrix repositories

Each machine needs two files from here:

  clients/<target>.sources  ->  /etc/apt/sources.list.d/packtrix.sources
  keys/` + KeyName + `.gpg    ->  ` + keyPath + `

then:

  apt-get update

A machine without internet access should have no other source: move the
distro's own entries, such as /etc/apt/sources.list.d/ubuntu.sources, aside.

`)
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TARGET\tSOURCES\tSUITE\tARCHITECTURES")
	for _, t := range targets {
		fmt.Fprintf(w, "%s\tclients/%s.sources\t%s\t%s\n", t.Name, t.Name, t.Codename, strings.Join(t.Architectures, ", "))
	}
	_ = w.Flush()
	fmt.Fprintf(&b, "\nThe repositories are signed with the key %s.\nCheck it before trusting it: gpg --show-keys keys/%s.asc\n", fingerprint, KeyName)
	if baseURL == "" {
		fmt.Fprintf(&b, "\nThe source entries use %s: replace it with where the\nrepositories are served, or set it with packtrix sync --base-url.\n", Placeholder)
	}
	return b.String()
}

// writeFile writes a file, creating its directory.
func writeFile(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	return os.WriteFile(name, data, 0o644)
}
