package v1alpha1

import (
	"errors"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		spec string
		// want lists expected "field: detail" fragments. Empty means valid.
		want []string
	}{
		{
			name: "valid",
			spec: `
  targets:
    - distro: ubuntu-24.04
      extraRepos:
        - {name: internal, type: apt, url: "https://pkgs.internal/apt", suite: stable, components: [main], gpgKey: k.gpg}
        - {name: flat, type: apt, url: "https://pkgs.k8s.io/deb/", suite: /, gpgKey: k.gpg}
    - name: oracle-9
      repos:
        - {name: ol9-baseos, type: rpm, url: "https://yum.oracle.com/OL9/baseos/{arch}/", gpgKey: ol.gpg}
  packages:
    - curl
    - {name: kubelet, version: "1.34.*", only: [ubuntu-24.04]}
    - {name: tool, except: [rpm]}
  resolve: {baseline: "lock:hosts/server01.lock"}
  updates: {check: "0 2 * * *", accept: security, publish: auto}
  publish: {signing: {key: env:PACKTRIX_SIGNING_KEY}}
  export: {splitSize: 4GiB}`,
		},
		{
			name: "no targets and no packages",
			spec: `
  targets: []
  packages: []`,
			want: []string{
				"spec.targets: at least one target is required",
				"spec.packages: at least one package is required",
			},
		},
		{
			name: "distro and repos together",
			spec: `
  targets:
    - distro: ubuntu-24.04
      repos: [{name: r, type: rpm, url: "https://x/", gpgKey: k}]
  packages: [curl]`,
			want: []string{"spec.targets[0]: set either distro or repos, not both"},
		},
		{
			name: "neither distro nor repos",
			spec: `
  targets: [{name: empty}]
  packages: [curl]`,
			want: []string{"spec.targets[0]: set distro"},
		},
		{
			name: "extraRepos without distro",
			spec: `
  targets:
    - name: custom
      extraRepos: [{name: r, type: rpm, url: "https://x/", gpgKey: k}]
  packages: [curl]`,
			want: []string{
				"spec.targets[0]: set distro",
				"spec.targets[0].extraRepos: only allowed with distro",
			},
		},
		{
			name: "target names",
			spec: `
  targets:
    - distro: ubuntu-24.04
    - distro: ubuntu-24.04
    - {name: deb, distro: debian-12}
    - {name: Rocky_9, distro: rocky-9}
    - repos: [{name: r, type: rpm, url: "https://x/", gpgKey: k}]
  packages: [curl]`,
			want: []string{
				`spec.targets[1].name: duplicate target name "ubuntu-24.04"`,
				`spec.targets[2].name: "deb" is reserved`,
				`spec.targets[3].name: invalid name "Rocky_9"`,
				"spec.targets[4].name: required",
			},
		},
		{
			name: "architectures",
			spec: `
  targets: [{distro: ubuntu-24.04, architectures: [amd64, x86_64, amd64]}]
  packages: [curl]`,
			want: []string{
				`spec.targets[0].architectures[1]: unsupported architecture "x86_64"`,
				`spec.targets[0].architectures[2]: duplicate architecture "amd64"`,
			},
		},
		{
			name: "apt repository fields",
			spec: `
  targets:
    - distro: ubuntu-24.04
      extraRepos:
        - {name: no-suite, type: apt, url: "https://x/", gpgKey: k}
        - {name: no-components, type: apt, url: "https://x/", suite: stable, gpgKey: k}
        - {name: flat, type: apt, url: "https://x/", suite: /, components: [main], gpgKey: k}
  packages: [curl]`,
			want: []string{
				"spec.targets[0].extraRepos[0].suite: required for apt repositories",
				"spec.targets[0].extraRepos[1].components: required for apt repositories",
				"spec.targets[0].extraRepos[2].components: must be empty for a flat repository",
			},
		},
		{
			name: "rpm repository fields",
			spec: `
  targets:
    - name: custom
      repos:
        - {name: a, type: rpm, url: "https://x/", suite: stable, components: [main], gpgKey: k}
        - {name: a, type: yum, url: "ftp://x/", gpgKey: ""}
        - {name: c, type: apt, url: "https://x/{release}/", suite: /, gpgKey: k}
  packages: [curl]`,
			want: []string{
				"spec.targets[0].repos[0].suite: only used by apt repositories",
				"spec.targets[0].repos[0].components: only used by apt repositories",
				`spec.targets[0].repos[1].name: duplicate repository name "a"`,
				`spec.targets[0].repos[1].type: must be apt or rpm, got "yum"`,
				`spec.targets[0].repos[1].url: must be an http, https or file URL`,
				"spec.targets[0].repos[1].gpgKey: required",
				"spec.targets[0].repos[2].type: all repositories of a target must have the same type, found rpm and apt",
				`spec.targets[0].repos[2].url: unknown placeholder`,
			},
		},
		{
			name: "distro object",
			spec: `
  targets:
    - distro:
        name: rhel-9
        components: [baseos, baseos]
        mirror: "nexus.internal/rhel"
        auth: {username: me}
  packages: [curl]`,
			want: []string{
				`spec.targets[0].distro.components[1]: duplicate component "baseos"`,
				`spec.targets[0].distro.mirror: must be an http, https or file URL`,
				"spec.targets[0].distro.auth: username and password must be set together",
			},
		},
		{
			name: "auth",
			spec: `
  targets:
    - distro:
        name: rhel-9
        auth: {}
    - distro:
        name: rhel-8
        auth: {username: me, password: "hunter2", token: "env:TOKEN", clientCert: c.pem}
  packages: [curl]`,
			want: []string{
				"spec.targets[0].distro.auth: set at least one credential",
				"spec.targets[1].distro.auth: set either password or token, not both",
				"spec.targets[1].distro.auth: clientCert and clientKey must be set together",
				`spec.targets[1].distro.auth.password: must be "env:NAME" or "file:/path"`,
			},
		},
		{
			name: "packages",
			spec: `
  targets: [{distro: ubuntu-24.04}]
  packages:
    - curl
    - curl
    - {name: "bad name"}
    - {name: x, deb: "a b", rpm: "c d", version: "1 2"}
    - {name: yy, only: [ubuntu-gpu], except: [deb]}
    - {name: z, repo: Nginx_Org}`,
			want: []string{
				`spec.packages[1].name: duplicate package "curl" (also at spec.packages[0])`,
				`spec.packages[2].name: invalid package name "bad name"`,
				`spec.packages[3].deb: invalid package name "a b"`,
				`spec.packages[3].rpm: invalid package name "c d"`,
				`spec.packages[3].version: must not contain spaces`,
				"spec.packages[4]: set either only or except, not both",
				`spec.packages[4].only[0]: unknown target "ubuntu-gpu" (targets: ubuntu-24.04; or deb, rpm)`,
				`spec.packages[5].repo: invalid repository name "Nginx_Org"`,
			},
		},
		{
			name: "settings",
			spec: `
  targets: [{distro: ubuntu-24.04}]
  packages: [curl]
  autoRepos: {deny: ["epel-[", ""]}
  resolve: {baseline: "lock:", alternatives: some, exclude: [x, x]}
  updates: {check: daily, accept: most, publish: never, minAge: "-1h"}
  publish: {keepSnapshots: -1, signing: {key: "env:1BAD"}}
  export: {format: zip, splitSize: 1KiB}`,
			want: []string{
				`spec.autoRepos.deny[0]: invalid pattern "epel-["`,
				`spec.autoRepos.deny[1]: invalid pattern ""`,
				`spec.resolve.baseline: must be "minimal", "none" or "lock:<path>", got "lock:"`,
				`spec.resolve.alternatives: must be one of first, all, got "some"`,
				`spec.resolve.exclude[1]: duplicate package name "x"`,
				`spec.updates.check: must be a cron expression with 5 fields`,
				`spec.updates.accept: must be one of all, security, none, got "most"`,
				"spec.updates.minAge: must not be negative",
				`spec.updates.publish: must be one of approval, auto, got "never"`,
				"spec.publish.keepSnapshots: must be 0 or more",
				`spec.publish.signing.key: invalid environment variable name "1BAD"`,
				`spec.export.format: must be one of tar.zst, got "zip"`,
				"spec.export.splitSize: must be at least 1MiB, got 1KiB",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := Parse([]byte(header + "spec:" + tt.spec + "\n"))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			r.Default()
			err = r.Validate()
			if len(tt.want) == 0 {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				return
			}
			var verr ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Validate error = %v, want a ValidationError", err)
			}
			if len(verr) != len(tt.want) {
				t.Errorf("got %d problems, want %d:\n%v", len(verr), len(tt.want), err)
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("missing %q in:\n%v", w, err)
				}
			}
		})
	}
}

func TestValidateTypeMeta(t *testing.T) {
	r := &OfflineRepository{
		Metadata: ObjectMeta{Name: "x"},
		Spec: OfflineRepositorySpec{
			Targets:  []Target{{Distro: &Distro{Name: "ubuntu-24.04"}}},
			Packages: []Package{{Name: "curl"}},
		},
	}
	r.Default()
	err := r.Validate()
	for _, w := range []string{`apiVersion: must be "packtrix.dev/v1alpha1"`, `kind: must be "OfflineRepository"`} {
		if err == nil || !strings.Contains(err.Error(), w) {
			t.Errorf("error = %v, want it to contain %q", err, w)
		}
	}
}
