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

package v1alpha1

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

// applyDefaults defaults r through a scheme, as decoding does, so the test
// also covers how the defaulting functions are registered.
func applyDefaults(t *testing.T, r *OfflineRepository) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	scheme.Default(r)
}

func TestDefaults(t *testing.T) {
	r := decode(t, header+`spec:
  targets:
    - distro: ubuntu-24.04
    - name: oracle-9
      repos: [{name: ol9, type: rpm, url: "https://yum.oracle.com/{arch}/", gpgKey: k}]
  packages: [curl]
`)
	applyDefaults(t, r)
	s := r.Spec

	if got := s.Targets[0].Name; got != "ubuntu-24.04" {
		t.Errorf("target name = %q, want the distro name", got)
	}
	for i, tg := range s.Targets {
		if !reflect.DeepEqual(tg.Architectures, []string{ArchAMD64}) {
			t.Errorf("targets[%d].architectures = %v, want [amd64]", i, tg.Architectures)
		}
	}
	checks := []struct {
		field     string
		got, want any
	}{
		{"autoRepos.enabled", *s.AutoRepos.Enabled, true},
		{"resolve.baseline", s.Resolve.Baseline, BaselineMinimal},
		{"resolve.alternatives", s.Resolve.Alternatives, AlternativesFirst},
		{"resolve.recommends", s.Resolve.Recommends, false},
		{"updates.accept", s.Updates.Accept, AcceptAll},
		{"updates.publish", s.Updates.Publish, PublishApproval},
		{"publish.path", s.Publish.Path, DefaultPublishPath},
		{"publish.keepSnapshots", *s.Publish.KeepSnapshots, DefaultKeepSnapshots},
		{"export.format", s.Export.Format, ExportFormatTarZst},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.field, c.got, c.want)
		}
	}
}

func TestDefaultsKeepExplicitValues(t *testing.T) {
	r := decode(t, header+`spec:
  targets: [{name: gpu, distro: ubuntu-24.04, architectures: [arm64]}]
  packages: [curl]
  autoRepos: {enabled: false}
  publish: {keepSnapshots: 0}
`)
	applyDefaults(t, r)
	s := r.Spec
	if s.Targets[0].Name != "gpu" || s.Targets[0].Architectures[0] != ArchARM64 {
		t.Errorf("target = %+v, explicit name and architectures were overwritten", s.Targets[0])
	}
	if *s.AutoRepos.Enabled {
		t.Error("autoRepos.enabled = true, want the explicit false")
	}
	if *s.Publish.KeepSnapshots != 0 {
		t.Errorf("keepSnapshots = %d, want the explicit 0", *s.Publish.KeepSnapshots)
	}
}
