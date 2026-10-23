# Roadmap

> **Status: proposal.** Phases describe priorities, not dates.

## Phase 0: Design *(current)*

- [x] Problem statement, goals and non-goals
- [x] Manifest draft (`packtrix.io/v1alpha1`)
- [x] Architecture and trust model
- [ ] Feedback from real air-gapped environments
- [ ] Decide the [open questions](#open-questions) that block Phase 1

## Phase 1: APT end to end (v0.1.0)

Goal: one manifest produces working, signed APT repositories on a local disk, for Ubuntu and Debian.

- Manifest parsing, defaults, validation, JSON Schema
- Catalog: `ubuntu-22.04`, `ubuntu-24.04`, `debian-12`, `debian-13`
- Dependency resolution in Go for APT, checked nightly against `apt`
- Download, upstream signature verification, content-addressed pool
- APT metadata subsetting and signing
- `packtrix.lock`
- Generated client files (`clients/`) and keys (`keys/`)
- Commands: `init`, `validate`, `render`, `plan`, `sync`
- Integration tests: install from the generated repositories in clean containers of each distro

RPM targets are valid in a manifest, but `plan` reports them as not supported yet.

## Phase 2: RPM (v0.2.0)

Goal: the same manifest also produces working, signed RPM repositories, with the same resolver and pipeline.

- Catalog: `rocky-9`, `almalinux-9`
- Dependency resolution in Go for RPM: capabilities, file dependencies, boolean dependencies, EL8 modules. Checked nightly against `dnf`.
- Package signature verification against the vendors' keys
- RPM metadata subsetting and signing, and `.repo` client files
- Integration tests for each distro

## Phase 3: Air gap and updates (v0.3.0)

- Snapshots, atomic publication, rollback, garbage collection
- `export` and `import` with full and delta bundles, `splitSize`
- `check`, lockfile diffs in `plan`, `updates.accept: security`, `minAge`
- `serve`
- `sync --verify-install`

## Phase 4: Catalog growth (v0.4.0)

- Vendor repositories with auto-detection: `docker`, `kubernetes@<minor>`, `cri-o@<minor>`, `nvidia-container-toolkit`, `epel-<major>`
- More distros: Rocky/Alma 8 and 10, CentOS Stream, RHEL (with subscription `auth`), Ubuntu 26.04
- `arm64`
- Nightly catalog checks in CI; `catalog update`; signed catalog releases

## Phase 5: Discovery

- `packtrix discover` for the local machine and over SSH
- Reproduce mode (pinned versions, using snapshot archives such as `snapshot.debian.org` and `snapshot.ubuntu.com` when old versions are gone) and maintain mode (newest versions)
- `baseline: lock:<file>` from a discovered host

## Later

- Kubernetes: a `CronJob` example first; an operator only if real users need it
- Bundles as OCI artifacts, to travel through registries next to container images
- A libsolv-based resolver, if the built-in closure meets cases it can't handle
- Locally built packages added to the output (`createrepo_c`)
- Organization catalogs

## Open questions

| # | Question | Current leaning |
|---|---|---|
| 1 | **What does `exclude` mean** when an excluded package is a dependency of a listed one? | Treat it as "provided by the baseline": don't download it, and report it in `plan` |
| 2 | **Default for APT alternatives** (`a \| b`) | `first`, with `all` available |
| 3 | **Per-target version pins**, e.g. different `kubelet` versions per target | Not in v1alpha1; use separate manifests if needed |
| 4 | **Per-target update policy** | Not in v1alpha1 |
| 5 | **Multi-arch APT** (foreign architectures such as `i386`) | Out of scope for v1alpha1 |
| 6 | **EL8 modular content** | Carry `modules.yaml` through; needs testing with real AppStream content |
| 7 | **API group domain** for `apiVersion` | Decided: `packtrix.io` |
| 8 | **Bundle format** | Plain `tar.zst` first, OCI artifact later |
