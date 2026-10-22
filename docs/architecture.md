# Architecture

> **Status: design draft.** This describes the intended implementation, for contributors and reviewers.

## Pipeline

```
manifest ──► expand ──► resolve ──► lock ──► fetch ──► build ──► validate ──► publish ──► export
              │           │                    │         │          │            │
         built-in     apt / dnf in         verify     subset     install      atomic
         catalog      a container per      upstream   upstream   test in a    switch of
                      target               signatures metadata,  clean        `current`
                                           + SHA-256  sign       container
```

| Stage | Input | Output |
|---|---|---|
| **expand** | Manifest and built-in catalog | Every target with concrete repository URLs, components and pinned keys |
| **resolve** | Expanded targets and package list | For each target and architecture: the exact set of packages to install |
| **lock** | Resolution result | `packtrix.lock` |
| **fetch** | Lockfile | Package files in the content-addressed pool, each verified |
| **build** | Pool and lockfile | Repository metadata for each target, signed |
| **validate** | Candidate snapshot | Proof that `apt`/`dnf` can install every listed package from it |
| **publish** | Validated snapshot | `current` switched to the new snapshot |
| **export** | Snapshot (and optionally an earlier lockfile) | A full or delta bundle |

## Resolution

Packtrix doesn't implement a dependency solver. For each target and architecture it runs the distro's own resolver inside a throwaway container of that distro:

- **APT targets:** `apt-get` runs against the target's repositories with the baseline's package status as the "installed" state, and prints what it would download (URL, size, hash) instead of installing.
- **RPM targets:** `dnf` runs with an install root set to the baseline and prints the resolved package set.

Why containers:

- **Correct by construction.** `apt` handles alternatives, virtual packages and `Pre-Depends` exactly as the machines will. `dnf` handles weak dependencies and modular content (EL8) exactly as the machines will.
- **Cross-distro.** A Rocky target can be resolved on an Ubuntu build machine, and the other way round.
- **Baselines for free.** `minimal` is the distro's minimal container image; `none` is an empty package status; a discovered host's status can be replayed.

The resolver's only output is a list of `(name, version, architecture, source repository)`. Everything else is done in Go.

An in-process resolver based on libsolv is a possible later option for environments without a container runtime. See [roadmap](roadmap.md).

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

Before publishing, Packtrix starts a clean container of each target distro, points it only at the candidate snapshot, and asks `apt`/`dnf` to install every listed package (a dry run). If any install fails, the snapshot isn't published.

## Bundles

A bundle is a `tar.zst` archive containing:

- the snapshot's metadata and the package files it needs (a delta bundle contains only the files the receiver doesn't have yet);
- the lockfile;
- a signed bundle manifest listing every file and its SHA-256.

`import` verifies the bundle signature and every file hash, writes into the pool, and publishes the snapshot the same atomic way.

## Code layout (proposed)

```
cmd/packtrix/          CLI entry point
api/v1alpha1/          manifest types, defaults, validation
internal/catalog/      built-in distros and vendor repositories, auto-detection
internal/resolve/      resolver interface; apt and dnf container implementations
internal/fetch/        download, verification, content-addressed pool
internal/repo/apt/     APT metadata subsetting and signing
internal/repo/rpm/     RPM metadata subsetting and signing
internal/lock/         lockfile read, write, diff
internal/publish/      snapshots, atomic switch, garbage collection
internal/bundle/       export and import
internal/validate/     install tests against candidate snapshots
```

## Requirements

| Side | Needs |
|---|---|
| Connected | Linux, `podman` or `docker`, outbound access to the upstream repositories (directly or through a proxy) |
| Offline | The `packtrix` binary (for `import`); any static web server to serve the files |
