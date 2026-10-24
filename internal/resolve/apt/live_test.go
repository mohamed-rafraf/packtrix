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

package apt

import (
	"compress/gzip"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/packtrix/packtrix/internal/deb"
	"github.com/packtrix/packtrix/internal/resolve"
)

// download reads a Packages.gz index. It doesn't verify it against the
// signed Release file: reading upstream repositories is #19.
func download(t *testing.T, url string) []*deb.Package {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("%s: %v", url, err)
	}
	var packages []*deb.Package
	err = deb.ReadParagraphs(gz, func(p deb.Paragraph) error {
		pkg, err := deb.ParsePackage(p)
		packages = append(packages, pkg)
		return err
	})
	if err != nil {
		t.Fatalf("%s: %v", url, err)
	}
	return packages
}

func TestLiveUbuntu2404(t *testing.T) {
	if testing.Short() {
		t.Skip("downloads the Ubuntu 24.04 indexes")
	}
	const archive = "https://archive.ubuntu.com/ubuntu/dists/"
	release := download(t, archive+"noble/main/binary-amd64/Packages.gz")
	var packages []*deb.Package
	for _, suite := range []string{"noble-security", "noble-updates", "noble"} {
		for _, component := range []string{"main", "universe"} {
			if suite == "noble" && component == "main" {
				packages = append(packages, release...)
				continue
			}
			packages = append(packages, download(t, archive+suite+"/"+component+"/binary-amd64/Packages.gz")...)
		}
	}
	ix := NewIndex("amd64", packages)

	minimal, err := MinimalBaseline("amd64", release)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("minimal baseline: %d packages", len(minimal))
	inMinimal := map[string]bool{}
	for _, p := range minimal {
		inMinimal[p.String()] = true
	}
	// The noble suite never changes, so neither do its versions.
	for _, want := range []string{"libc6=2.39-0ubuntu8", "dpkg=1.22.6ubuntu6", "apt=2.7.14build2"} {
		if !inMinimal[want] {
			t.Errorf("the minimal baseline doesn't contain %s", want)
		}
	}

	var listed []*deb.Package
	for _, name := range []string{"curl", "nginx", "docker.io"} {
		listed = append(listed, pick(t, ix, name))
	}
	for _, tt := range []struct {
		baseline string
		packages []*deb.Package
		want     []string
	}{
		{"none", nil, []string{"libcurl4t64", "libc6", "nginx-common", "containerd", "runc", "iptables"}},
		{"minimal", minimal, []string{"libcurl4t64", "nginx-common", "containerd", "runc", "iptables"}},
	} {
		t.Run(tt.baseline, func(t *testing.T) {
			result, err := resolve.Closure(NewModel(ix, tt.packages, Options{}), listed)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%d packages from %d in the indexes", len(result.Packages), len(packages))
			found := map[string]*deb.Package{}
			for _, p := range result.Packages {
				if found[p.Name] != nil {
					t.Errorf("two versions of %s: %s and %s", p.Name, found[p.Name], p)
				}
				found[p.Name] = p
				if inMinimal[p.String()] && tt.baseline == "minimal" {
					t.Errorf("%s is in the baseline", p)
				}
			}
			for _, name := range tt.want {
				if p := found[name]; p == nil {
					t.Errorf("the closure doesn't contain %s", name)
				} else {
					t.Log(result.Why(p))
				}
			}
		})
	}
}
