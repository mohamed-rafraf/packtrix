# Architecture

> **Status: design draft.** This describes the intended implementation, for contributors and reviewers.

## Pipeline

```
manifest ──► expand ──► resolve ──► lock ──► fetch ──► build ──► publish ──► export
              │           │                    │         │          │
         built-in     dependency           verify     subset     atomic
         catalog      closure in Go,       upstream   upstream   switch of
                      from upstream        signatures metadata,  `current`
                      metadata             + SHA-256  sign
```

| Stage | Input | Output |
|---|---|---|
| **expand** | Manifest and built-in catalog | Every target with concrete repository URLs, components and pinned keys |
| **resolve** | Expanded targets, package list and upstream metadata | For each target and architecture: every package the machines need beyond the baseline |
| **lock** | Resolution result | `packtrix.lock` |
| **fetch** | Lockfile | Package files in the content-addressed pool, each verified |
| **build** | Pool and lockfile | Repository metadata for each target, signed |
| **validate** *(optional)* | Candidate snapshot | Proof that `apt`/`dnf` install every listed package from it |
| **publish** | Built snapshot | `current` switched to the new snapshot |
| **export** | Snapshot (and optionally an earlier lockfile) | A full or delta bundle |

## Resolution

Packtrix resolves dependencies itself, in Go, from the metadata of each target's repositories: `Packages` files for APT, `primary.xml` and `filelists.xml` for RPM. It needs no container runtime and no `apt` or `dnf` binary, so a Rocky target resolves on an Ubuntu machine and the other way round.

### A repository, not an installation

`apt` and `dnf` choose exactly one consistent set of packages to install on one machine. Packtrix builds a repository that machines install from later, with their own `apt` or `dnf`. The repository must contain everything a machine could need, and the machine makes the final choice. So Packtrix computes a dependency **closure**, not a single solution:

1. Start with the listed packages. A version constraint such as `1.34.*` selects an exact version.
2. For each dependency of each package in the set, skip it if the baseline or the set already satisfies it. Otherwise add a provider: the first alternative of `a | b` (every alternative with `alternatives: all`), a real package before a virtual one, and the newest version that satisfies the constraint.
3. When a package breaks or conflicts with the baseline version of another package, add the newer version that the machine's `apt` or `dnf` will upgrade to.
4. Repeat until nothing is added.

An extra package costs a little space; a missing one makes an installation fail offline. Where the two could differ, Packtrix includes rather than omits.

The resolver's output is a list of `(name, version, architecture, source repository)`, which the lockfile records.

### What the resolver handles

| APT | RPM |
|---|---|
| `Depends` and `Pre-Depends`, alternatives (`a \| b`), versions compared with dpkg's rules | `Requires` on capabilities and `Provides`, versions compared with RPM's rules, including epochs |
| Virtual packages through `Provides` | File dependencies, such as `/usr/bin/python3`, through `filelists.xml` |
| `Breaks` and `Conflicts` against baseline versions | Boolean dependencies: `(a or b)`, `(x if y)` |
| `Multi-Arch` and `Architecture: all` | Modular content on EL8: packages of streams that aren't enabled are hidden, as `dnf` hides them |
| `Recommends`, when `resolve.recommends` is on | Weak dependencies, when `resolve.recommends` is on; `Obsoletes` |

### The engine and the models

The closure loop, in `internal/resolve`, knows no package format. It runs once per target and architecture, on packages that are exact versions, and asks a **model** for two things: what a package needs, and whether the baseline has a package. The APT and RPM models translate their formats into needs:

- A need lists groups of candidates. The model orders each group by preference: a real package before a virtual one, the newest version first. If any candidate is already in the set or the baseline, the need is satisfied. Otherwise the engine adds the first candidate of the first non-empty group, or of every group with `alternatives: all`.
- A need can have a condition: a list of packages. It only applies once the set or the baseline contains one of them, even if that happens later in the loop.
- A need without any candidate is a failure. The engine collects every failure with its path from the listed package, and records why each package is in the set, such as `libfoo1 ← nginx ← listed`.

| Dependency | Need |
|---|---|
| APT `Depends: a (>= 1) \| b` | One group for `a`: its versions from 1 on, then the packages that provide `a`. Then one group for `b` |
| APT `Breaks: libfoo (<< 2.0)` | Condition: the versions of `libfoo` before 2.0. Candidate: the newest `libfoo` |
| RPM `Requires: libcrypto.so.3()(64bit)` | One group: the packages that provide the capability |
| RPM `Requires: /usr/bin/python3` | One group: the packages whose file list contains the path |
| RPM `(a or b)` | Two groups, as for APT's `a \| b` |
| RPM `(a and b)` | Two needs |
| RPM `(x if y)` | Condition: the packages that provide `y`. Candidates: those that provide `x` |
| RPM `(x unless y)` | `x`, without a condition: the engine has no negative condition, so it includes rather than omits |

So the RPM model plugs in without changing the engine.

### Baselines from metadata

The baseline is computed from the target's own metadata, at the versions of the distro's **release**, not its latest updates:

- **`minimal`, APT:** `Essential` packages and those with `Priority: required` or `important`, which is what the distro's installer puts on a minimal system.
- **`minimal`, RPM:** the `@core` group from the repositories' `comps.xml`.
- **`none`:** nothing.
- **`lock:<file>`:** the package list of a discovered host, at its exact versions.

Release versions matter for machines installed from older media: if a package needs a newer `libssl3` than the release shipped, that update is included.

### Checking against apt and dnf

The resolver must agree with the tools the machines run:

- **Nightly, in CI:** for every built-in distro, test manifests are resolved by Packtrix and by real `apt` and `dnf` in containers. The check fails if `apt` or `dnf` picks a package that Packtrix's closure doesn't contain.
- **Optionally, on your side:** `packtrix sync --verify-install` runs the [validation](#validation) step when `podman` or `docker` is available.

Containers are used for these checks only. Producing repositories never needs them.

## Fetching and the package pool

- Every package file is checked against the SHA-256 in the upstream index, and that index is checked against the upstream signature (see [Security](security.md)).
- Files are stored once, by hash: `pool/sha256/ab/abcdef…`. Snapshots and targets reference the pool. The same `kubelet.deb` from the Kubernetes repository is stored once even when three Debian-family targets use it.
- Downloads are resumable and cached across runs. A `sync` after a small upstream change downloads only the changed files.

## Building repository metadata

Packtrix publishes a **subset** of upstream repositories. Instead of regenerating metadata by reading every package file, it copies the matching entries from the upstream metadata:

- **APT:** the stanzas for the selected packages are copied from upstream `Packages` files, and Packtrix writes a new `Release`/`InRelease` file with its own signature.
- **RPM:** the entries for the selected packages are copied from upstream `primary`, `filelists` and `other` metadata (plus `updateinfo` and `modules` where present), and Packtrix writes a new `repomd.xml`.

This keeps upstream fields intact, is deterministic, and doesn't depend on `apt-ftparchive` or `createrepo_c` being installed. `createrepo_c` is only needed for packages that don't come from an upstream repository (a planned feature).

For each target, the distro packages and the vendor packages are merged into **one** repository, so every machine needs a single source entry.

## Publication

```
/srv/repos/
├── current -> snapshots/2026-10-22T0200Z
├── snapshots/
│   ├── 2026-10-15T0200Z/
│   └── 2026-10-22T0200Z/
│       ├── apt/ubuntu/dists/noble/…  apt/ubuntu/pool/…
│       ├── rpm/rocky/9/x86_64/repodata/…  …/Packages/…
│       ├── keys/
│       ├── clients/
│       └── packtrix.lock
└── pool/sha256/…
```

- A snapshot is fully built and validated before anything points at it.
- `current` switches with a single atomic rename, so clients never see a mix of old and new.
- APT repositories set `Acquire-By-Hash: yes`, and earlier metadata files stay reachable for a grace period. A client that fetched the old `InRelease` just before the switch can still download the matching `Packages` file.
- Garbage collection removes pool files that no kept snapshot references.

## Validation

With `packtrix sync --verify-install`, Packtrix starts a clean container of each target distro before publishing, points it only at the candidate snapshot, and installs every listed package with `apt` or `dnf`. If any install fails, the snapshot isn't published.

This step is optional, and it is the only one that needs `podman` or `docker`. The same install tests always run in Packtrix's own CI.

## Bundles

A bundle is a `tar.zst` archive containing:

- the snapshot's metadata and the package files it needs (a delta bundle contains only the files the receiver doesn't have yet);
- the lockfile;
- a signed bundle manifest listing every file and its SHA-256.

`import` verifies the bundle signature and every file hash, writes into the pool, and publishes the snapshot the same atomic way.

## Code layout

Implemented:

```
cmd/packtrix/                   binary entry point
internal/cli/                   commands (cobra)
internal/version/               build information
internal/deb/                   Debian versions and how they sort, dependency fields, Packages indexes, version constraints
internal/resolve/               dependency closure, independent of the package format
internal/resolve/apt/           APT dependency model
pkg/apis/packtrix/              internal manifest API, used by the rest of Packtrix
pkg/apis/packtrix/v1alpha1/     manifest format v1alpha1: types, defaults, generated conversions
pkg/apis/packtrix/scheme/       registers every API version
pkg/apis/packtrix/validation/   validation of the internal API
pkg/apis/packtrix/manifest/     reading and writing manifest files
```

The manifest API follows the Kubernetes pattern. A manifest is decoded in its own version, defaulted with that version's defaults, converted to the internal API, then validated. A future version adds a package next to `v1alpha1/` and registers it in the scheme; the rest of Packtrix doesn't change. The deepcopy, defaulting and conversion code (`zz_generated.*.go`) is generated by `make update-codegen`.

Planned:

```
internal/catalog/      built-in distros and vendor repositories, auto-detection
internal/resolve/      RPM dependency model, baselines
internal/fetch/        download, verification, content-addressed pool
internal/repo/apt/     APT metadata subsetting and signing
internal/repo/rpm/     RPM metadata subsetting and signing
internal/lock/         lockfile read, write, diff
internal/publish/      snapshots, atomic switch, garbage collection
internal/bundle/       export and import
internal/validate/     optional install tests against candidate snapshots, in containers
```

## Requirements

| Side | Needs |
|---|---|
| Connected | The `packtrix` binary and outbound access to the upstream repositories (directly or through a proxy). `podman` or `docker` only for `--verify-install`. |
| Offline | The `packtrix` binary (for `import`); any static web server to serve the files |
