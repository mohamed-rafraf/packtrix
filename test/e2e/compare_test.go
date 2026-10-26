//go:build e2e

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

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
)

// comparison is a set of packages that TestCompare resolves.
type comparison struct {
	name     string
	packages []string
	// distros limits the set to these distros; empty means every distro.
	distros []string
}

func comparisons(t *testing.T) []comparison {
	t.Helper()
	sets := []comparison{
		{name: "server", packages: []string{
			"ca-certificates", "chrony", "cron", "curl", "git", "htop", "jq", "less", "logrotate",
			"nginx", "openssh-server", "rsync", "rsyslog", "sudo", "tmux", "vim",
		}},
		// Alternatives, such as default-mta | mail-transport-agent and the
		// PHP SAPIs, and virtual packages, such as java and the Apache API.
		{name: "alternatives", packages: []string{
			"bsd-mailx", "build-essential", "default-jre-headless", "libapache2-mod-php",
			"mariadb-server", "php-cli", "postgresql", "python3-pip",
		}},
	}
	m, err := manifest.LoadFile("../../examples/k8s-node-prereqs.yaml")
	if err != nil {
		t.Fatal(err)
	}
	k8s := comparison{name: "k8s"}
	for _, p := range m.Spec.Packages {
		k8s.packages = append(k8s.packages, p.Name)
	}
	for _, target := range m.Spec.Targets {
		k8s.distros = append(k8s.distros, target.Distro.Name)
	}
	return append(sets, k8s)
}

// TestCompare resolves sets of packages with Packtrix, and with the real apt
// of each distro in a clean container, and fails if apt installs a package
// that Packtrix's closure doesn't contain: a repository built from the
// closure would miss it. Both resolve from the same repositories, and
// Packtrix from nothing (baseline none), so the closure also holds what the
// container already has.
func TestCompare(t *testing.T) {
	sets := comparisons(t)
	for _, d := range distros {
		t.Run(d.name, func(t *testing.T) {
			t.Parallel()
			cache := t.TempDir()
			for _, s := range sets {
				if len(s.distros) > 0 && !slices.Contains(s.distros, d.name) {
					continue
				}
				t.Run(s.name, func(t *testing.T) {
					compare(t, d.name, d.image, s.packages, cache)
				})
			}
		})
	}
}

// planned is what TestCompare reads of packtrix plan -o json.
type planned struct {
	Targets []struct {
		Repositories []struct {
			URL        string
			Suite      string
			Components []string
			Release    string
		}
		Packages []struct {
			Name    string
			Version string
			Size    int64
		}
	}
}

// installed is a package that apt installs, or that the container has.
type installed struct{ name, version string }

func compare(t *testing.T, distro, image string, pkgs []string, cache string) {
	// Upstream can publish between plan and apt-get update: plan again after
	// apt, and start over if the repositories changed meanwhile.
	for attempt := 1; ; attempt++ {
		p := plan(t, distro, pkgs, cache)
		apt, has := aptInstall(t, distro, image, p, pkgs)
		if again := plan(t, distro, pkgs, cache); !slices.Equal(releases(p), releases(again)) {
			if attempt == 3 {
				t.Fatal("the upstream repositories changed during each of 3 comparisons")
			}
			t.Log("The upstream repositories changed during the comparison; comparing again.")
			continue
		}
		check(t, p, apt, has)
		return
	}
}

// check fails for each package that apt installs and the closure doesn't
// contain, and reports what the closure has beyond apt and the container.
func check(t *testing.T, p *planned, apt, has []installed) {
	closure := map[string]string{}
	sizes := map[string]int64{}
	for _, pkg := range p.Targets[0].Packages {
		closure[pkg.Name], sizes[pkg.Name] = pkg.Version, pkg.Size
	}
	for _, a := range apt {
		switch version, ok := closure[a.name]; {
		case !ok:
			t.Errorf("apt installs %s %s, which Packtrix's closure doesn't contain", a.name, a.version)
		case version != a.version:
			t.Errorf("apt installs %s %s, and Packtrix's closure has %s", a.name, a.version, version)
		}
	}
	known := map[string]bool{}
	for _, i := range slices.Concat(apt, has) {
		known[i.name] = true
	}
	var beyond []string
	var size int64
	for name := range closure {
		if !known[name] {
			beyond = append(beyond, name)
			size += sizes[name]
		}
	}
	slices.Sort(beyond)
	t.Logf("apt installs %d packages; Packtrix's closure has %d, of which %d (%.1f MiB) neither apt installs nor the container has: %s",
		len(apt), len(closure), len(beyond), float64(size)/(1<<20), strings.Join(beyond, " "))
}

// plan resolves pkgs with packtrix plan, for the host's architecture.
func plan(t *testing.T, distro string, pkgs []string, cache string) *planned {
	t.Helper()
	dir := t.TempDir()
	m := fmt.Sprintf(`apiVersion: packtrix.io/v1alpha1
kind: OfflineRepository
metadata:
  name: compare
spec:
  targets:
    - distro: %s
      architectures: [%s]
  packages: [%s]
  resolve:
    baseline: none
`, distro, runtime.GOARCH, strings.Join(pkgs, ", "))
	if err := os.WriteFile(filepath.Join(dir, "packtrix.yaml"), []byte(m), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(t.Context(), binary, "plan", "-f", filepath.Join(dir, "packtrix.yaml"), "--cache-dir", cache, "-o", "json")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("packtrix plan: %v\n%s", err, stderr.Bytes())
	}
	var p planned
	if err := json.Unmarshal(stdout.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Targets) != 1 {
		t.Fatalf("plan has %d targets, want 1", len(p.Targets))
	}
	return &p
}

func releases(p *planned) []string {
	var out []string
	for _, r := range p.Targets[0].Repositories {
		out = append(out, r.Release)
	}
	return out
}

// inst matches the packages in apt-get's simulation, such as
// "Inst libc6 [2.39-0ubuntu8.3] (2.39-0ubuntu8.4 Ubuntu:24.04/noble-updates [amd64])".
var inst = regexp.MustCompile(`^Inst (\S+) (?:\[\S+\] )?\((\S+) [^)]*\)`)

// aptInstall simulates installing pkgs with apt, from the repositories that
// Packtrix read, in a clean container of image. It returns the packages that
// apt installs, and those the container has.
func aptInstall(t *testing.T, distro, image string, p *planned, pkgs []string) (apt, has []installed) {
	t.Helper()
	// Clean containers have no certificate authorities, and the signatures
	// protect the content: apt uses plain HTTP.
	family, _, _ := strings.Cut(distro, "-")
	var sources strings.Builder
	for _, r := range p.Targets[0].Repositories {
		fmt.Fprintf(&sources, "Types: deb\nURIs: %s\nSuites: %s\nComponents: %s\nSigned-By: /usr/share/keyrings/%s-archive-keyring.gpg\n\n",
			strings.Replace(r.URL, "https://", "http://", 1), r.Suite, strings.Join(r.Components, " "), family)
	}
	file := filepath.Join(t.TempDir(), "compare.sources")
	if err := os.WriteFile(file, []byte(sources.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	// Packtrix picks the newest version, so apt must not hold back the
	// phased updates of Ubuntu.
	script := `set -eu
rm -f /etc/apt/sources.list
find /etc/apt/sources.list.d -type f ! -name compare.sources -delete
apt-get update -qq
dpkg-query -W -f 'Has ${Package} ${Version}\n'
apt-get install -s --no-install-recommends -o APT::Get::Always-Include-Phased-Updates=true ` + strings.Join(pkgs, " ") + "\n"
	out := container(t, image, map[string]string{file: "/etc/apt/sources.list.d/compare.sources"}, script)
	for line := range strings.Lines(out) {
		line = strings.TrimSpace(line)
		if m := inst.FindStringSubmatch(line); m != nil {
			name, _, _ := strings.Cut(m[1], ":")
			apt = append(apt, installed{name, m[2]})
		} else if f := strings.Fields(line); len(f) == 3 && f[0] == "Has" {
			has = append(has, installed{f[1], f[2]})
		}
	}
	if len(apt) == 0 {
		t.Fatalf("apt installs nothing:\n%s", out)
	}
	return apt, has
}

// container runs a script in a clean container of image, with files mounted
// read-only at the given paths, and returns its output.
func container(t *testing.T, image string, mounts map[string]string, script string) string {
	t.Helper()
	docker := dockerCommand()
	switch {
	case docker == "" && os.Getenv("CI") != "":
		t.Fatal("docker isn't available")
	case docker == "":
		t.Skip("docker isn't available")
	}
	args := []string{"run", "--rm"}
	for src, dst := range mounts {
		args = append(args, "-v", src+":"+dst+":ro")
	}
	args = append(args, image, "sh", "-c", script)
	out, err := exec.CommandContext(t.Context(), docker, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker run %s: %v\n%s", image, err, out)
	}
	return string(out)
}
