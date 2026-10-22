# Workflow

> **Status: design draft.** Commands and flags are proposals.

Packtrix runs in two places:

```
        CONNECTED SIDE                                       OFFLINE SIDE
  (build machine or CI, has internet)                  (no internet access)
┌──────────────────────────────────────┐           ┌──────────────────────────────┐
│ packtrix.yaml + packtrix.lock (git)  │           │ packtrix import              │
│ packtrix plan                        │  bundle   │   verify signature           │
│ packtrix sync                        │ ────────► │   add snapshot, switch       │
│ packtrix export                      │  USB,     │ packtrix serve  (or nginx)   │
│                                      │  diode, … │                              │
└──────────────────────────────────────┘           └──────────────────────────────┘
```

- **Connected side:** Linux with `podman` or `docker`, used for dependency resolution. This is where the manifest, the lockfile and the downloads live.
- **Offline side:** only the `packtrix` binary to import bundles. Any web server can serve the result.

## Commands

| Command | Side | What it does |
|---|---|---|
| `packtrix init` | connected | Write a starter manifest, e.g. `--target ubuntu-24.04 --target rocky-9` |
| `packtrix validate` | both | Check the manifest without network access |
| `packtrix render` | connected | Print the fully expanded manifest: every built-in name replaced by its URLs, components and key fingerprints |
| `packtrix plan` | connected | Resolve dependencies, write `packtrix.lock`, and print what changes and how much will be downloaded |
| `packtrix sync` | connected | Carry out the lockfile: download, verify, build a snapshot, validate it, publish it |
| `packtrix check` | connected | A cheap check of whether any upstream repository changed. Exit code `0` = no change, `2` = changes, `1` = error. |
| `packtrix export` | connected | Write a bundle: full, or `--since <old lockfile>` for a delta |
| `packtrix import` | offline | Verify a bundle and add it as a new snapshot |
| `packtrix serve` | offline | Serve the repositories over HTTP, for small setups and testing |
| `packtrix rollback` | both | Point `current` back at an earlier snapshot |
| `packtrix catalog list \| show \| update` | connected | Inspect or refresh the built-in catalog |
| `packtrix discover` *(planned)* | connected | Inspect an existing host over SSH and write a manifest for it |

## Day 1: build and ship

```console
# connected side
$ packtrix init --target ubuntu-24.04 --target rocky-9 > packtrix.yaml
$ vi packtrix.yaml                     # list the packages
$ packtrix plan                        # resolve; review what will be downloaded
$ packtrix sync                        # download, verify, build signed repositories
$ packtrix export -o bundle-2026-10-22.tar.zst

# carry the bundle across the air gap

# offline side
$ packtrix import bundle-2026-10-22.tar.zst --to /srv/repos
$ packtrix serve /srv/repos            # or point any web server at /srv/repos/current
```

`plan` shows each target, where every package comes from, and any vendor repository that was added automatically:

```
rocky-9  (Rocky Linux 9 · rpm · x86_64)
  repos    baseos, appstream              distro
         + docker                         auto ← docker-ce
         + kubernetes@1.34                auto ← kubelet
  packages to download: …
```

## Configuring the machines

`sync` also writes ready-made client configuration under `clients/`, one file per target, plus the public keys under `keys/`:

```
/srv/repos/current/
├── apt/        ubuntu/…  debian/…
├── rpm/        rocky/9/x86_64/…
├── keys/       packtrix-repo.gpg, vendor keys
└── clients/    ubuntu-24.04.sources, rocky-9.repo
```

On each machine, copy two files: its client file and the repository key.

```
# /etc/apt/sources.list.d/packtrix.sources   (Ubuntu 24.04)
Types: deb
URIs: http://repo.internal/apt/ubuntu
Suites: noble
Components: main
Signed-By: /etc/apt/keyrings/packtrix-repo.gpg
```

All packages for a target, from the distro and from vendors, are merged into one repository. Each machine therefore needs a single source entry.

## Day 2: updates

```console
$ packtrix check                       # exit 2: something changed upstream
$ packtrix plan                        # new lockfile, printed as a diff
ubuntu-24.04
  ~ openssl      3.0.13-0ubuntu3.4 → 3.0.13-0ubuntu3.5   [security]
  + libfoo1      1.2-1                                    new dependency of nginx
  - libbar0      0.9-2                                    no longer required
$ git commit packtrix.lock             # open a pull request; a reviewer approves the diff
$ packtrix sync
$ packtrix export --since <previous packtrix.lock> -o delta-2026-10-29.tar.zst

# offline side: import the delta; the previous snapshot stays available for rollback
$ packtrix import delta-2026-10-29.tar.zst --to /srv/repos
```

### Approval through Git

When `updates.publish` is `approval`, the lockfile in Git is the approval gate:

1. A scheduled CI job runs `packtrix check` and, if something changed, `packtrix plan`.
2. The job opens a pull request with the new `packtrix.lock`.
3. A reviewer reads the diff: what changes, which repositories and keys are new.
4. Merging the pull request triggers `packtrix sync` and `packtrix export`.

No server or operator is needed. This is the same model as dependency-update bots.

## Deployment setups

| Setup | Where Packtrix runs | How content crosses |
|---|---|---|
| **Fully air-gapped** | Connected build machine or CI | Bundle on approved media or through a data diode |
| **DMZ / semi-connected** | A host in the DMZ | Internal machines read directly from its web server |
| **GitOps** | CI pipeline | Bundles published as CI artifacts, then moved by your transfer process |
| **Kubernetes** *(later)* | A `CronJob` running the same CLI | Same as above |
