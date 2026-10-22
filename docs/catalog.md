# Built-in catalog

> **Status: design draft.** The lists below are the intended initial scope.

The **catalog** is Packtrix's built-in knowledge of repositories: URLs, components, architectures and pinned signing keys. It lets a manifest say `distro: rocky-9` instead of listing URLs and key files, and lets Packtrix find vendor repositories on its own.

## Built-in distros

Used with `targets[].distro`:

| Family | Names | Notes |
|---|---|---|
| Ubuntu | `ubuntu-22.04`, `ubuntu-24.04`, `ubuntu-26.04` | `amd64` comes from `archive.ubuntu.com` and `security.ubuntu.com`; `arm64` from `ports.ubuntu.com`. The catalog handles the difference. |
| Debian | `debian-12`, `debian-13` | |
| Rocky Linux | `rocky-8`, `rocky-9`, `rocky-10` | |
| AlmaLinux | `almalinux-8`, `almalinux-9`, `almalinux-10` | |
| CentOS Stream | `centos-stream-9`, `centos-stream-10` | CentOS Linux 7 and 8 are end-of-life and not included |
| RHEL | `rhel-8`, `rhel-9`, `rhel-10` | Requires `distro.auth` with your subscription certificates |

Every distro entry defines its official repositories (for Ubuntu: release, `-updates`, `-security`), default components and signing keys.

### Components

`distro.components` selects parts of the distribution. It is the same field for every family:

| | Ubuntu | Debian | Rocky / Alma / RHEL / CentOS Stream |
|---|---|---|---|
| **Default** | `main`, `universe` | `main` | `baseos`, `appstream` |
| **Optional** | `restricted`, `multiverse` | `contrib`, `non-free`, `non-free-firmware` | `crb`, `extras`, `highavailability` |

The catalog maps these to vendor-specific names. For example, `crb` becomes `powertools` on EL8 and `codeready-builder-…` on RHEL. Ubuntu defaults to `main` plus `universe` because many common server packages, such as `glusterfs-client` on 24.04, live in `universe`.

## Built-in vendor repositories

Added automatically when a listed package needs them (see [auto-detection](#auto-detection)):

| Name | Provides | deb | rpm | Notes |
|---|---|---|---|---|
| `docker` | `docker-ce`, `containerd.io`, plugins | ✅ | ✅ | |
| `kubernetes@<minor>` | `kubelet`, `kubeadm`, `kubectl`, `cri-tools`, `kubernetes-cni` | ✅ | ✅ | One repository per Kubernetes minor version; chosen by the package `version` |
| `cri-o@<minor>` | `cri-o` | ✅ | ✅ | One repository per minor version |
| `nvidia-container-toolkit` | `nvidia-container-toolkit` and its libraries | ✅ | ✅ | |
| `epel-<major>` | Extra packages for Enterprise Linux | — | ✅ | Community repository. Turns on `crb` automatically, which EPEL requires. |

**Later**, based on demand: `helm`, `hashicorp`, `postgresql`, `grafana`, `nginx-org`.

Container tools such as `podman`, `buildah` and `skopeo` are already in the distro repositories and need no vendor repository.

## Auto-detection

You list packages, not vendor repositories. For each target, Packtrix decides where each package comes from:

```
for each package in the manifest:
  1. available in the distro (or in your extraRepos)?   → use it; the distro always wins
  2. otherwise, look it up in the catalog for this distro and architecture:
       no repository provides it        → error, with "did you mean" suggestions
       exactly one repository           → add that repository
       a versioned family (kubernetes)  → choose by the package's `version`; no version → error
       several unrelated repositories   → error: ambiguous, set `repo:`
then add any repository the chosen ones require (epel → crb) and resolve dependencies.
```

Rules that keep this predictable:

| Rule | Why |
|---|---|
| **The distro always wins** | `nginx` exists in Ubuntu and on nginx.org. Silently swapping in a third-party build would change what you run. Opt in per package with `repo: nginx-org`. |
| **Versioned repositories need a version** | Picking a Kubernetes minor version silently could break version-skew rules. `version: "1.34.*"` selects `kubernetes@1.34`. |
| **Only top-level packages trigger detection** | Dependencies must resolve from the repositories already chosen. Vendor packages depend on their own repository or the distro, so this covers real cases. |
| **Everything is visible** | `plan` lists each repository that was added automatically and which package caused it, and flags new signing keys. The lockfile records the source of every package. |
| **Organizations can refuse** | `autoRepos.deny: [epel-*]` blocks a repository. `autoRepos.enabled: false` turns detection off completely. |
| **Only built-in distros** | Targets defined from scratch with `repos` have no catalog mapping, so detection is off for them. |

### Where the data comes from

- Each catalog release ships a name index: which package names each vendor repository provides, for each distro and architecture.
- At `plan` time, Packtrix also reads the live index of each candidate repository, so packages that a vendor added after the catalog release are still found.

## Catalog entry format

Catalog entries use the same fields as custom repositories, pre-filled:

```yaml
# catalog/vendors/kubernetes.yaml
name: kubernetes
params:
  version: { required: true, pattern: '^1\.\d+$' }
deb:
  url: "https://pkgs.k8s.io/core:/stable:/v{version}/deb/"
  suite: "/"
rpm:
  url: "https://pkgs.k8s.io/core:/stable:/v{version}/rpm/"
keys: [kubernetes-release]          # → catalog/keys/, pinned by fingerprint
```

Vendor URL layouts are irregular, and absorbing that is the catalog's job. For example, Docker uses its `centos` path for Rocky and AlmaLinux, but its `rhel` path for RHEL.

## Inclusion policy

A repository is added to the catalog only if:

- it is the **official** repository of the vendor or project, not a mirror, PPA or personal repository;
- it is signed, and its key fingerprint is published in the vendor's own documentation;
- it is used widely enough to justify maintenance.

## Maintenance

Repositories move and keys rotate. For example, Kubernetes moved from `apt.kubernetes.io` to `pkgs.k8s.io` in 2023, and the old repositories were later shut down. The catalog is maintained like this:

- **Nightly checks in CI:** for every entry, fetch the index, verify its signature with the pinned key, and resolve a test package. Broken entries are found before users hit them.
- **Separate release cycle:** the catalog is versioned independently of the binary. `packtrix catalog update` fetches a newer signed catalog, and a snapshot is embedded in each binary so it works offline.
- **Pinned in the lockfile:** the catalog version is recorded, so updating the catalog never changes an existing lockfile silently.
- **Organization catalogs** *(planned):* `--catalog <path or URL>` adds your own entries next to the built-in ones.
