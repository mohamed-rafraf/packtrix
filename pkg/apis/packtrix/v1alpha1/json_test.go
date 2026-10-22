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
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/yaml"
)

const header = `apiVersion: packtrix.io/v1alpha1
kind: OfflineRepository
metadata:
  name: test
`

// decode decodes a manifest strictly, without defaults.
func decode(t *testing.T, manifest string) *OfflineRepository {
	t.Helper()
	var r OfflineRepository
	if err := yaml.UnmarshalStrict([]byte(manifest), &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

func TestDecodeShortAndLongForms(t *testing.T) {
	r := decode(t, header+`spec:
  targets:
    - distro: ubuntu-24.04
    - distro:
        name: rocky-9
        components: [baseos, appstream, crb]
  packages:
    - curl
    - name: kubelet
      version: "1.34.*"
    - name: apache
      deb: apache2
      rpm: httpd
  updates:
    minAge: 72h
  export:
    splitSize: 4GiB
`)
	s := r.Spec
	wantTargets := []Target{
		{Distro: &Distro{Name: "ubuntu-24.04"}},
		{Distro: &Distro{Name: "rocky-9", Components: []string{"baseos", "appstream", "crb"}}},
	}
	if !reflect.DeepEqual(s.Targets, wantTargets) {
		t.Errorf("targets = %+v, want %+v", s.Targets, wantTargets)
	}
	wantPackages := []Package{
		{Name: "curl"},
		{Name: "kubelet", Version: "1.34.*"},
		{Name: "apache", Deb: "apache2", RPM: "httpd"},
	}
	if !reflect.DeepEqual(s.Packages, wantPackages) {
		t.Errorf("packages = %+v, want %+v", s.Packages, wantPackages)
	}
	if time.Duration(s.Updates.MinAge) != 72*time.Hour {
		t.Errorf("minAge = %v, want 72h", time.Duration(s.Updates.MinAge))
	}
	if s.Export.SplitSize != 4<<30 {
		t.Errorf("splitSize = %d, want %d", s.Export.SplitSize, 4<<30)
	}
}

// Short forms must survive a round trip, so printed manifests stay readable.
func TestMarshalShortForms(t *testing.T) {
	r := decode(t, header+`spec:
  targets:
    - distro: ubuntu-24.04
    - distro: {name: rocky-9, components: [baseos]}
  packages:
    - curl
    - {name: kubelet, version: "1.34"}
`)
	out, err := yaml.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"- distro: ubuntu-24.04\n",
		"- curl\n",
		"name: rocky-9",
		`version: "1.34"`,
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	again := decode(t, string(out))
	if !reflect.DeepEqual(again, r) {
		t.Errorf("after a round trip:\n%+v\nwant:\n%+v", again, r)
	}
}
