# Roadmap

> **Status: proposal.** Phases describe priorities, not dates.

## Phase 0: Design *(current)*

- [x] Problem statement, goals and non-goals
- [x] Manifest draft (`packtrix.dev/v1alpha1`)
- [x] Architecture and trust model
- [ ] Feedback from real air-gapped environments
- [ ] Decide the [open questions](#open-questions) that block Phase 1

## Phase 1: First end-to-end slice

Goal: one manifest produces working, signed APT and RPM repositories on a local disk.

- Manifest parsing, defaults, validation, JSON Schema
- Catalog: `ubuntu-22.04`, `ubuntu-24.04`, `debian-12`, `debian-13`, `rocky-9`, `almalinux-9`
- Container-based resolution for APT and RPM
- Download, upstream signature verification, content-addressed pool
- APT and RPM metadata subsetting and signing
- `packtrix.lock`
- Commands: `init`, `validate`, `render`, `plan`, `sync`
- Integration tests: install from the generated repositories in clean containers of each distro

## Phase 2: Air gap and updates

- Snapshots, atomic publication, rollback, garbage collection
- `export` and `import` with full and delta bundles, `splitSize`
- `check`, lockfile diffs in `plan`, `updates.accept: security`, `minAge`
- Generated client files (`clients/`)
- `serve`

## Phase 3: Catalog growth

- Vendor repositories with auto-detection: `docker`, `kubernetes@<minor>`, `cri-o@<minor>`, `nvidia-container-toolkit`, `epel-<major>`
- More distros: Rocky/Alma 8 and 10, CentOS Stream, RHEL (with subscription `auth`), Ubuntu 26.04
- `arm64`
- Nightly catalog checks in CI; `catalog update`; signed catalog releases

## Phase 4: Discovery

- `packtrix discover` for the local machine and over SSH
- Reproduce mode (pinned versions, using snapshot archives such as `snapshot.debian.org` and `snapshot.ubuntu.com` when old versions are gone) and maintain mode (newest versions)
- `baseline: lock:<file>` from a discovered host

## Later

- Kubernetes: a `CronJob` example first; an operator only if real users need it
- Bundles as OCI artifacts, to travel through registries next to container images
- In-process resolver (libsolv) for build machines without a container runtime
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
| 7 | **API group domain** for `apiVersion` | `packtrix.dev`, if the domain is registered |
| 8 | **Bundle format** | Plain `tar.zst` first, OCI artifact later |
