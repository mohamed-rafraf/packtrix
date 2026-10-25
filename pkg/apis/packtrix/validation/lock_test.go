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

package validation

import (
	"strings"
	"testing"

	"github.com/packtrix/packtrix/pkg/apis/packtrix"
)

func validLock() *packtrix.Lock {
	return &packtrix.Lock{Spec: packtrix.LockSpec{
		Manifest: "sha256:" + strings.Repeat("0", 64),
		Catalog:  "2026.10.1",
		Targets: []packtrix.LockTarget{{
			Name: "ubuntu-24.04", Architecture: "amd64", Format: packtrix.RepoTypeAPT,
			Repositories: []packtrix.LockRepository{{
				Name: "release", URL: "https://archive.ubuntu.com/ubuntu/", Suite: "noble",
				Release: "sha256:" + strings.Repeat("1", 64), Signer: "F6ECB3762474EDA9D21B7022871920D1991BC93C",
			}},
			Packages: []packtrix.LockPackage{{
				Name: "curl", Version: "8.5.0", Architecture: "amd64", Repository: "release",
				URL: "https://archive.ubuntu.com/ubuntu/pool/main/c/curl/curl_8.5.0_amd64.deb", SHA256: strings.Repeat("ab", 32), Size: 1000,
			}},
		}},
	}}
}

func TestValidateLock(t *testing.T) {
	if err := ValidateLock(validLock()); err != nil {
		t.Errorf("valid lock: %v", err)
	}

	l := validLock()
	l.Spec.Manifest = "md5:abc"
	l.Spec.Catalog = ""
	l.Spec.Targets = append(l.Spec.Targets, l.Spec.Targets[0], packtrix.LockTarget{Name: "Bad", Architecture: "i386", Format: "deb"})
	l.Spec.Targets[1].Repositories = []packtrix.LockRepository{{Name: "release", URL: "ftp://x", Release: "sha256:short", Signer: "f6ec"}}
	l.Spec.Targets[1].Packages = []packtrix.LockPackage{{Name: "", Repository: "nowhere", URL: "", SHA256: "AB", Size: -1, Epoch: -1}}
	err := ValidateLock(l)
	for _, want := range []string{
		`spec.manifest: must be a digest, "sha256:<hex>", got "md5:abc"`,
		"spec.catalog: required",
		"spec.targets[1]: duplicate target ubuntu-24.04 (amd64)",
		"spec.targets[1].repositories[0].url: ",
		`spec.targets[1].repositories[0].release: must be a digest, "sha256:<hex>", got "sha256:short"`,
		`spec.targets[1].repositories[0].signer: must be a key fingerprint, got "f6ec"`,
		"spec.targets[1].packages[0].name: required",
		"spec.targets[1].packages[0].version: required",
		"spec.targets[1].packages[0].architecture: required",
		`spec.targets[1].packages[0].repository: unknown repository "nowhere"`,
		"spec.targets[1].packages[0].url: required",
		`spec.targets[1].packages[0].sha256: must be a SHA-256 in hex, got "AB"`,
		"spec.targets[1].packages[0].size: must not be negative",
		"spec.targets[1].packages[0].epoch: must not be negative",
		"spec.targets[2].name: invalid name",
		`spec.targets[2].architecture: unsupported architecture "i386"`,
		`spec.targets[2].format: must be apt or rpm, got "deb"`,
	} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v\nwant it to contain %q", err, want)
		}
	}

	bad := validLock()
	bad.Spec.Targets[0].Packages[0].Name = "Not a package"
	if err := ValidateLock(bad); err == nil || !strings.Contains(err.Error(), `invalid package name "Not a package"`) {
		t.Errorf("error = %v", err)
	}
}
