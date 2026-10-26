# Getting started

This guide builds offline APT repositories for Ubuntu 24.04 and Debian 12 machines, from start to finish: install Packtrix, create a signing key, write a manifest, plan, sync, serve the result, and install packages on a machine. It takes about ten minutes, and downloads about 40 MB: 30 MB of upstream indexes, and the packages.

> **What works today:** Ubuntu 22.04 and 24.04, Debian 12 and 13, on amd64 and arm64. Planned: RPM distributions, vendor repositories that Packtrix adds by itself (Docker, Kubernetes, …), and bundles to carry across the air gap. See the [roadmap](roadmap.md).

## What you need

- A Linux or macOS machine with Internet access: the **connected side**. It reaches the official archives (archive.ubuntu.com, security.ubuntu.com, deb.debian.org, security.debian.org) directly, or through the proxy in `HTTPS_PROXY`.
- Go 1.26 or newer, to install Packtrix. There is no binary release yet.
- GnuPG, once, to create the key that signs your repositories.
- On the offline side, any web server, to serve the repositories to the machines.

## 1. Install Packtrix

```console
$ go install github.com/packtrix/packtrix/cmd/packtrix@latest
$ packtrix --help
```

From a clone of the repository, `make build` writes `bin/packtrix` instead.

## 2. Create a signing key

Packtrix signs the repositories it builds, and the machines trust them through your public key. Create a key for this purpose only:

```console
$ export GNUPGHOME=$(mktemp -d)      # don't touch your personal keyring
$ gpg --batch --passphrase '' --quick-gen-key 'Packtrix Repository Signing <repo@example.org>' rsa3072 sign never
$ gpg --armor --export-secret-keys > signing-key.asc
$ chmod 600 signing-key.asc
```

The key must be RSA of at least 3072 bits, or Ed25519, and must not be protected by a passphrase (support for passphrases is planned). Keep `signing-key.asc` secret: whoever has it can sign packages your machines will trust. In CI, put it in a secret and reference it as `env:PACKTRIX_SIGNING_KEY` instead of a file.

## 3. Write a manifest

Create `packtrix.yaml` next to the key:

```yaml
apiVersion: packtrix.io/v1alpha1
kind: OfflineRepository
metadata:
  name: my-servers
spec:
  targets:                  # the kinds of machines you run
    - distro: ubuntu-24.04
    - distro: debian-12
  packages:                 # what they need; dependencies come with them
    - curl
    - jq
    - nginx
    - rsync
  publish:
    signing:
      key: file:signing-key.asc
```

- `targets` names the distributions. `packtrix catalog list` shows the built-in ones, and `packtrix catalog show ubuntu-24.04` their repositories and the keys Packtrix trusts for them.
- `packages` is one list for every target. A package can be limited to some targets, have a version constraint such as `"1.34.*"`, or a different name per format: see the [manifest reference](manifest.md#specpackages).
- The **baseline** is what the machines already have. By default, `minimal`, Packtrix assumes a minimal installation of the distro, from its release media, and only downloads what that lacks. If the machines are containers, which have less, add `resolve: { baseline: none }`: everything, down to the C library, goes into the repository.

Check the manifest, and see the repositories it uses:

```console
$ packtrix validate
packtrix.yaml is valid: 2 target(s), 4 package(s)
$ packtrix render
```

## 4. Plan

`plan` reads the metadata of every repository, checks its signature against the pinned keys, and resolves the packages and their dependencies for each target. It downloads no package. The indexes it reads are cached (in `~/.cache/packtrix` on Linux, or `--cache-dir`), so the next `plan` only fetches what changed upstream.

```console
$ packtrix plan
Reading ubuntu-24.04 (amd64): release, https://archive.ubuntu.com/ubuntu/ noble
Reading ubuntu-24.04 (amd64): updates, https://archive.ubuntu.com/ubuntu/ noble-updates
Reading ubuntu-24.04 (amd64): security, https://security.ubuntu.com/ubuntu/ noble-security
Reading debian-12 (amd64): release, https://deb.debian.org/debian/ bookworm
…
Wrote packtrix.lock
ubuntu-24.04 (amd64): Ubuntu noble, 16 packages, 2.8 MiB
  REPOSITORY  SUITE           PACKAGES  SIGNED BY
  release     noble           70854     F6ECB3762474EDA9D21B7022871920D1991BC93C
  updates     noble-updates   15230     F6ECB3762474EDA9D21B7022871920D1991BC93C
  security    noble-security  11770     F6ECB3762474EDA9D21B7022871920D1991BC93C

  PACKAGE              VERSION                            SIZE       WHY
  curl                 8.5.0-2ubuntu10.16                 221.1 KiB  listed
  jq                   1.7.1-3ubuntu0.24.04.2             64.2 KiB   listed
  nginx                1.24.0-2ubuntu7.18                 512.3 KiB  listed
  rsync                3.2.7-1ubuntu1.5                   432.6 KiB  listed
  libcurl4t64          8.5.0-2ubuntu10.16                 336.3 KiB  curl
  libjq1               1.7.1-3ubuntu0.24.04.2             138.6 KiB  jq
  nginx-common         1.24.0-2ubuntu7.18                 44.0 KiB   nginx
  …
  libsasl2-modules-db  2.1.28+dfsg1-5ubuntu3.1            19.9 KiB   curl → libcurl4t64 → libldap2 → libsasl2-2

debian-12 (amd64): Debian bookworm, 16 packages, 2.9 MiB
…

2 target(s), 32 package files to download, 5.7 MiB
```

For each target, `plan` shows the repositories it read, with the key that signed each, and the packages to download. `WHY` is the chain that brings a package in: `libsasl2-modules-db` is there because `curl` needs `libcurl4t64`, which needs `libldap2`, and so on. Packages the baseline already has aren't listed.

The result goes to **`packtrix.lock`**: the exact version, URL and SHA-256 of every package, and the key that signed each repository. Commit the manifest and the lock: `sync` downloads exactly what the lock says, and an update is a diff of the lock that you can review.

If a package doesn't exist, or a dependency can't be satisfied, `plan` says which, with the chain of packages that needs it, and writes no lock.

## 5. Sync

`sync` carries out the lock: it downloads the packages, checks each one against the lock, and builds one signed repository per target. `--base-url` is the URL where the machines will reach the result.

```console
$ packtrix sync --base-url http://repo.internal
Downloaded debian-12 (amd64): 16 packages, 16 new, 2.9 MiB
Downloaded ubuntu-24.04 (amd64): 16 packages, 16 new, 2.8 MiB
TARGET        SUITE     ARCHITECTURES  PACKAGES  SIZE
debian-12     bookworm  amd64          16        2.9 MiB
ubuntu-24.04  noble     amd64          16        2.8 MiB

Published repos/current (snapshot 2026-10-26T161035Z).
Each machine needs two files: see repos/current/clients/README.
```

Everything is in `publish.path`, `./repos` by default:

```
repos/
├── current -> snapshots/2026-10-26T161035Z    what the machines use
├── snapshots/2026-10-26T161035Z/
│   ├── apt/ubuntu-24.04/                     one signed repository per target
│   ├── apt/debian-12/
│   ├── clients/                              a source entry per target, and a README
│   ├── keys/                                 your public key
│   └── packtrix.lock
└── pool/                                     every package file, once
```

Each `sync` builds a new snapshot, then switches `current` to it in one step: machines never see a half-built repository, and a `sync` that fails leaves the published one as it was. A second `sync` downloads only what changed.

## 6. Serve the repositories

Serve `repos/current` at the base URL with any web server. With nginx:

```nginx
server {
    listen 80;
    server_name repo.internal;
    root /srv/repos/current;
}
```

To try it out, `python3 -m http.server 8080 --directory repos/current` serves it on port 8080 (then use `--base-url http://<host>:8080`).

Plain HTTP is fine: the machines check every file against your signature, not against the transport. See [security](security.md#https).

**Across the air gap:** bundles (`packtrix export` and `import`) are planned. Until then, copy the published directory with any tool, and serve it on the other side:

```console
$ tar -C repos/current -czf my-servers.tar.gz .
# offline side
$ mkdir -p /srv/repos/current && tar -C /srv/repos/current -xzf my-servers.tar.gz
```

## 7. Configure the machines

Each machine needs two files from `repos/current`: its target's source entry, `clients/<target>.sources`, and the public key, `keys/packtrix-repo.gpg`. `clients/README` lists them, with the fingerprint of the key. Copy them to the machine the way you deploy other configuration (scp, Ansible, an image build), and put them in place:

```console
# on an Ubuntu 24.04 machine
$ sudo install -D -m 644 packtrix-repo.gpg /etc/apt/keyrings/packtrix-repo.gpg
$ sudo install -m 644 ubuntu-24.04.sources /etc/apt/sources.list.d/packtrix.sources
```

```
# /etc/apt/sources.list.d/packtrix.sources
Types: deb
URIs: http://repo.internal/apt/ubuntu-24.04/
Suites: noble
Components: main
Signed-By: /etc/apt/keyrings/packtrix-repo.gpg
```

A machine without Internet access should have no other source, or `apt-get update` reports errors for them. Move the distro's own entries aside: `/etc/apt/sources.list.d/ubuntu.sources` on Ubuntu 24.04, `/etc/apt/sources.list` on Ubuntu 22.04, `/etc/apt/sources.list.d/debian.sources` on Debian. Then:

```console
$ sudo apt-get update
$ sudo apt-get install curl jq nginx rsync
```

`apt` installs from your repository like from any other: the listed packages, and the dependencies that the machine doesn't have yet.

## 8. Update

Upstream publishes fixes all the time. To bring them in, plan again: the lock changes, and its diff shows what moved.

```console
$ packtrix plan
$ git diff packtrix.lock         # review the new versions
$ packtrix sync --base-url http://repo.internal
```

`sync` publishes a new snapshot, and keeps the previous ones next to it (`publish.keepSnapshots`, 3 by default); they share the package files, so they take little space. The machines pick up the changes with `apt-get update` and `apt-get upgrade`. To add a package, add it to the manifest, then plan and sync.

## Troubleshooting

| Message | What to do |
|---|---|
| `unknown distro "ubuntu-24.4"; did you mean "ubuntu-24.04"?` | Use a name from `packtrix catalog list`. |
| `no package foo in the target's repositories` | The package doesn't exist in that distro, or is in a component the target doesn't use, such as `restricted`. Add the component with `distro: { name: …, components: [main, universe, restricted] }`, or limit the package to other targets with `only:`. |
| `… needs libfoo (>= 2), which no package provides` | A dependency can't be satisfied from the target's repositories. The chain shows which package needs it. |
| `the server's certificate isn't trusted …; pass its certificate with --ca-file` | A proxy or mirror uses a private certificate authority: pass its certificate with `--ca-file` to `plan` and `sync`. |
| `the lock was planned from another version of the manifest; run packtrix plan` | The manifest changed since the last `plan`. Plan again. |
| `the repository changed since the lock was planned, …; run packtrix plan` | `sync` runs on another machine than `plan`, or the cache was cleared, and upstream has moved on since. Plan again, or keep the cache (`--cache-dir`) between `plan` and `sync`. |
| `publish.signing.key is required to sign the repositories` | Add `publish.signing.key` to the manifest. |
| `another sync is writing repos` | Another `sync` uses the same `publish.path`. Wait for it. |

## Next

- [Concepts](concepts.md): targets, the baseline, the lock and snapshots, in more depth.
- [Manifest reference](manifest.md): every field, such as components, mirrors, extra repositories, and credentials.
- [Workflow](workflow.md): every command, and how updates are reviewed.
- [Security](security.md): what Packtrix verifies, and what your machines trust.
