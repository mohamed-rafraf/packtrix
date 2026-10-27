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

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"sigs.k8s.io/yaml"

	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/validation"
)

// schema compiles the committed schema.
func schema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	data, err := os.ReadFile("../../schemas/v1alpha1/offlinerepository.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(ID, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(ID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// check validates a YAML manifest against the schema.
func check(t *testing.T, s *jsonschema.Schema, data []byte) error {
	t.Helper()
	j, err := yaml.YAMLToJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(j))
	if err != nil {
		t.Fatal(err)
	}
	return s.Validate(v)
}

func TestUpToDate(t *testing.T) {
	want, err := os.ReadFile("../../schemas/v1alpha1/offlinerepository.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Generate("../../pkg/apis/packtrix/v1alpha1")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("schemas/v1alpha1/offlinerepository.json is out of date: run make update-schema")
	}
}

func TestExamples(t *testing.T) {
	s := schema(t)
	files, err := filepath.Glob("../../examples/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples: %v", err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := check(t, s, data); err != nil {
			t.Errorf("%s: %v", file, err)
		}
		if !bytes.Contains(data, []byte("# yaml-language-server: $schema="+ID+"\n")) {
			t.Errorf("%s doesn't point editors to the schema", file)
		}
	}
}

// The schema and packtrix validate agree on what is valid.
func TestAgreement(t *testing.T) {
	s := schema(t)
	for _, tc := range []struct {
		name, spec string
		valid      bool
	}{
		{"minimal", "targets: [{distro: ubuntu-24.04}]\n  packages: [curl]", true},
		{"objects", `targets:
    - name: edge
      distro: {name: debian-12, components: [main, contrib], mirror: "https://nexus.internal/repository/"}
      architectures: [amd64, arm64]
      extraRepos: [{name: tools, type: apt, url: "https://pkgs.internal/apt", suite: stable, components: [main], gpgKey: keys/tools.asc, auth: {token: "env:TOKEN"}}]
  packages:
    - curl
    - {name: apache, deb: apache2, rpm: httpd, only: [edge]}
    - {name: kubelet, version: "1.34.*", except: [deb]}
  resolve: {baseline: "lock:host.lock", alternatives: all, recommends: true, exclude: [nvidia-driver]}
  updates: {accept: security, minAge: 72h, publish: auto}
  publish: {path: /srv/repos, keepSnapshots: 0, signing: {key: "file:/etc/packtrix/key.asc"}}
  export: {splitSize: 4GiB}`, true},
		{"distro not built in yet", "targets: [{distro: rocky-9}]\n  packages: [curl]", true},
		{"size in bytes", "targets: [{distro: debian-13}]\n  packages: [curl]\n  export: {splitSize: 1073741824}", true},
		{"unknown field", "targets: [{distro: ubuntu-24.04}]\n  packages: [curl]\n  mirror: x", false},
		{"unknown package field", "targets: [{distro: ubuntu-24.04}]\n  packages: [{name: curl, versions: \"8\"}]", false},
		{"no packages", "targets: [{distro: ubuntu-24.04}]\n  packages: []", false},
		{"no targets", "targets: []\n  packages: [curl]", false},
		{"architecture", "targets: [{distro: ubuntu-24.04, architectures: [x86_64]}]\n  packages: [curl]", false},
		{"unquoted version", "targets: [{distro: ubuntu-24.04}]\n  packages: [{name: kubelet, version: 1.30}]", false},
		{"baseline", "targets: [{distro: ubuntu-24.04}]\n  packages: [curl]\n  resolve: {baseline: full}", false},
		{"secret", "targets: [{distro: ubuntu-24.04}]\n  packages: [curl]\n  publish: {signing: {key: s3cret}}", false},
		{"distro name", "targets: [{distro: Ubuntu}]\n  packages: [curl]", false},
		{"target name", "targets: [{name: Edge, distro: ubuntu-24.04}]\n  packages: [curl]", false},
		{"package name", "targets: [{distro: ubuntu-24.04}]\n  packages: [\"-curl\"]", false},
		{"repository type", "targets: [{repos: [{name: r, type: zypper, url: \"https://x\", gpgKey: k}]}]\n  packages: [curl]", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte("apiVersion: packtrix.io/v1alpha1\nkind: OfflineRepository\nmetadata: {name: test}\nspec:\n  " + tc.spec + "\n")
			schemaErr := check(t, s, data)
			m, err := manifest.Parse(data)
			if err == nil {
				err = validation.ValidateOfflineRepository(m)
			}
			if (schemaErr == nil) != tc.valid || (err == nil) != tc.valid {
				t.Errorf("valid = %t; schema: %v; packtrix: %v", tc.valid, schemaErr, err)
			}
		})
	}
}
