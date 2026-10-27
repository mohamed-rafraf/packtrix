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

// Package e2e installs from the repositories that Packtrix publishes, with
// the real apt, in clean containers of each distro: the only proof that a
// repository works. Run it with make test-e2e.
package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/packtrix/packtrix/internal/repo/apt/apttest"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
)

// binary is the packtrix binary that the tests run, built by TestMain.
var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "packtrix-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "packtrix")
	build := exec.CommandContext(context.Background(), "go", "build", "-o", binary, "../../cmd/packtrix")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "building packtrix:", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// distros are the distros of v0.1.0, with the image of a clean machine.
var distros = []struct{ name, image string }{
	{"ubuntu-22.04", "ubuntu:22.04"},
	{"ubuntu-24.04", "ubuntu:24.04"},
	{"debian-12", "debian:12"},
	{"debian-13", "debian:13"},
}

// packages are installed on every distro. ca-certificates and nginx run
// maintainer scripts, which add users and change owners; the others bring
// libraries along.
var packages = []string{"curl", "jq", "rsync", "ca-certificates", "nginx"}

func TestInstall(t *testing.T) {
	for _, d := range distros {
		t.Run(d.name, func(t *testing.T) {
			t.Parallel()
			r := publish(t, d.name, packages, nil)
			out, err := r.install(d.image, packages)
			if err != nil {
				t.Fatalf("installing from the repository: %v\n%s", err, out)
			}
			for _, line := range strings.Split(out, "\n") {
				if strings.HasPrefix(line, "W:") || strings.HasPrefix(line, "E:") {
					t.Errorf("apt: %s", line)
				}
			}
			t.Log(out)
		})
	}
}

// TestExample publishes examples/k8s-node-prereqs.yaml for each of its
// distros, and installs every one of its packages.
func TestExample(t *testing.T) {
	m, err := manifest.LoadFile("../../examples/k8s-node-prereqs.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range m.Spec.Packages {
		names = append(names, p.Name)
	}
	for _, target := range m.Spec.Targets {
		d := target.Distro.Name
		t.Run(d, func(t *testing.T) {
			t.Parallel()
			i := slices.IndexFunc(distros, func(x struct{ name, image string }) bool { return x.name == d })
			if i < 0 {
				t.Fatalf("no image for %s", d)
			}
			r := publish(t, d, names, nil)
			out, err := r.install(distros[i].image, names)
			if err != nil {
				t.Fatalf("installing from the repository: %v\n%s", err, out)
			}
			t.Log(out)
		})
	}
}

// A dependency missing from the repository makes the install fail: the
// test can tell a repository that works from one that doesn't.
func TestMissingDependency(t *testing.T) {
	t.Parallel()
	r := publish(t, "ubuntu-24.04", []string{"curl"}, []string{"libcurl4t64"})
	out, err := r.install("ubuntu:24.04", []string{"curl"})
	if err == nil {
		t.Fatalf("curl installed without libcurl4t64:\n%s", out)
	}
	if !strings.Contains(out, "libcurl4t64") {
		t.Errorf("apt didn't name the missing dependency:\n%s", out)
	}
}

// repository is a published directory, served over HTTP.
type repository struct {
	t      *testing.T
	dir    string
	target string
}

// publish plans and syncs a manifest of one distro, for the host's
// architecture, which its containers have too, and serves the result on
// the host's network, where containers reach it.
func publish(t *testing.T, distro string, pkgs, exclude []string) *repository {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "signing.asc"), apttest.PrivateKey(t), 0o600); err != nil {
		t.Fatal(err)
	}
	// Containers have less than a minimal installation of their distro.
	manifest := fmt.Sprintf(`apiVersion: packtrix.io/v1alpha1
kind: OfflineRepository
metadata:
  name: e2e
spec:
  targets:
    - distro: %s
      architectures: [%s]
  packages: [%s]
  resolve:
    baseline: none
    exclude: [%s]
  publish:
    signing:
      key: file:signing.asc
`, distro, runtime.GOARCH, strings.Join(pkgs, ", "), strings.Join(exclude, ", "))
	if err := os.WriteFile(filepath.Join(dir, "packtrix.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewUnstartedServer(nil)
	t.Cleanup(server.Close)
	cache := t.TempDir()
	run(t, "plan", "-f", filepath.Join(dir, "packtrix.yaml"), "--cache-dir", cache)
	run(t, "sync", "-f", filepath.Join(dir, "packtrix.yaml"), "--cache-dir", cache, "--base-url", "http://"+server.Listener.Addr().String())
	current := filepath.Join(dir, "repos", "current")
	server.Config.Handler = http.FileServer(http.Dir(current))
	server.Start()
	return &repository{t: t, dir: current, target: distro}
}

func run(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), binary, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("packtrix %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	t.Logf("packtrix %s:\n%s", args[0], out)
}

// dockerCommand returns the docker command, when it reaches a daemon.
var dockerCommand = sync.OnceValue(func() string {
	docker, err := exec.LookPath("docker")
	if err != nil || exec.CommandContext(context.Background(), docker, "version").Run() != nil {
		return ""
	}
	return docker
})

// install installs packages in a clean container of image, whose only
// source is the generated one, and returns apt's output.
func (r *repository) install(image string, pkgs []string) (string, error) {
	r.t.Helper()
	docker := dockerCommand()
	switch {
	case docker == "" && os.Getenv("CI") != "":
		r.t.Fatal("docker isn't available")
	case docker == "":
		r.t.Skip("docker isn't available: the repository was published, but not installed from")
	}
	script := `set -eu
rm -f /etc/apt/sources.list
find /etc/apt/sources.list.d -type f ! -name packtrix.sources -delete
apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y ` + strings.Join(pkgs, " ") + `
dpkg -s ` + strings.Join(pkgs, " ") + ` >/dev/null
`
	cmd := exec.CommandContext(r.t.Context(), docker, "run", "--rm", "--network", "host",
		"-v", filepath.Join(r.dir, "clients", r.target+".sources")+":/etc/apt/sources.list.d/packtrix.sources:ro",
		"-v", filepath.Join(r.dir, "keys", "packtrix-repo.gpg")+":/etc/apt/keyrings/packtrix-repo.gpg:ro",
		image, "sh", "-c", script)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
