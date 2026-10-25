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
	"slices"
	"testing"

	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/validation"
)

// TestLiveExample plans examples/k8s-node-prereqs.yaml against the real
// Ubuntu and Debian archives.
func TestLiveExample(t *testing.T) {
	if testing.Short() {
		t.Skip("reads the Ubuntu 22.04, Ubuntu 24.04 and Debian 12 archives")
	}
	m, err := manifest.LoadFile("../../examples/k8s-node-prereqs.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c, err := catalog.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	client, err := fetch.New(fetch.Options{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := Run(t.Context(), m, c, client, Options{Dir: "../../examples"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Targets) != 3 {
		t.Fatalf("%d targets planned, want 3", len(p.Targets))
	}
	digest, err := manifest.Digest(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.ValidateLock(p.Lock(m.Name, digest, c.Version)); err != nil {
		t.Errorf("the lock isn't valid: %v", err)
	}
	for _, target := range p.Targets {
		t.Logf("%s (%s): %s, %d packages, %d MiB", target.Name, target.Architecture, target.Distribution, len(target.Packages), target.Size()>>20)
		for _, want := range []string{"curl", "glusterfs-client", "open-vm-tools"} {
			if !slices.ContainsFunc(target.Packages, func(p Package) bool { return p.Name == want }) {
				t.Errorf("%s: no %s", target.Name, want)
			}
		}
	}
}
