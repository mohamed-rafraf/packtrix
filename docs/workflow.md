# Workflow

> **Status: partly implemented.** `init`, `validate`, `render`, `plan`, `sync` and `catalog list | show` work for Ubuntu and Debian. The rest is marked *planned*: a proposal that may change. To try what works, follow [Getting started](getting-started.md).

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

- **Connected side:** the `packtrix` binary, with access to the upstream repositories. This is where the manifest, the lockfile and the downloads live.
- **Offline side:** only the `packtrix` binary to import bundles. Any web server can serve the result.

## Commands

| Command | Side | What it does |
|---|---|---|
| `packtrix init` | connected | Write a starter manifest, e.g. `--target ubuntu-24.04 --target debian-12 --package curl`. It prints it, or writes it with `-f`, never over an existing file. |
| `packtrix validate` | both | Check the manifest without network access |
| `packtrix render` | connected | Print the fully expanded manifest: every built-in name replaced by its URLs, components and key fingerprints |
| `packtrix plan` | connected | Resolve dependencies, write `packtrix.lock`, and print each target's packages, why each is needed, and how much will be downloaded. `-o json` for scripts, `--ca-file` for a private certificate authority, `--lock` to write the lock elsewhere, `--cache-dir` for the cache of upstream indexes. Nothing is written unless every target is planned. |
| `packtrix sync` | connected | Carry out the lockfile: download, verify, build a snapshot, publish it. `--base-url` is where the published directory is served, for the client files. With `--verify-install` *(planned)*, first test-install it in containers. |
| `packtrix check` *(planned)* | connected | A cheap check of whether any upstream repository changed. Exit code `0` = no change, `2` = changes, `1` = error. |
| `packtrix export` *(planned)* | connected | Write a bundle: full, or `--since <old lockfile>` for a delta |
| `packtrix import` *(planned)* | offline | Verify a bundle and add it as a new snapshot |
| `packtrix serve` *(planned)* | offline | Serve the repositories over HTTP, for small setups and testing |
| `packtrix rollback` *(planned)* | both | Point `current` back at an earlier snapshot |
| `packtrix catalog list \| show` | connected | Inspect the built-in catalog. `catalog update`, to refresh it, is planned. |
| `packtrix discover` *(planned)* | connected | Inspect an existing host over SSH and write a manifest for it |

## Day 1: build and ship

```console
# connected side
$ packtrix init --target ubuntu-24.04 --target debian-12 > packtrix.yaml
$ vi packtrix.yaml                     # list the packages
$ packtrix plan                        # resolve; review what will be downloaded
$ packtrix sync --base-url http://repo.internal   # download, verify, build signed repositories
$ packtrix export -o bundle-2026-10-22.tar.zst    # planned

# carry the bundle across the air gap

# offline side
$ packtrix import bundle-2026-10-22.tar.zst --to /srv/repos   # planned
$ packtrix serve /srv/repos            # planned; or point any web server at /srv/repos/current
```

Until `export` and `import` exist, copy the published directory across with any tool, as [Getting started](getting-started.md#6-serve-the-repositories) shows.

`plan` shows each target, the repositories it read with the key that signed each, and the packages to download with the reason each is needed:

```
ubuntu-24.04 (amd64): Ubuntu noble, 16 packages, 2.8 MiB
  REPOSITORY  SUITE           PACKAGES  SIGNED BY
  release     noble           70854     F6ECB3762474EDA9D21B7022871920D1991BC93C
  updates     noble-updates   15230     F6ECB3762474EDA9D21B7022871920D1991BC93C
  security    noble-security  11770     F6ECB3762474EDA9D21B7022871920D1991BC93C

  PACKAGE              VERSION                            SIZE       WHY
  curl                 8.5.0-2ubuntu10.16                 221.1 KiB  listed
  libcurl4t64          8.5.0-2ubuntu10.16                 336.3 KiB  curl
  libldap2             2.6.10+dfsg-0ubuntu0.24.04.1       193.0 KiB  curl → libcurl4t64
  …
```

Once vendor repositories are added automatically *(planned)*, they appear in the repository table, with the package that needed each.

## Configuring the machines

`sync` also writes ready-made client configuration under `clients/`, one file per target, plus the public keys under `keys/`:

```
/srv/repos/current/
├── apt/        ubuntu-24.04/…  debian-12/…   one repository per target
├── rpm/        rocky-9/…                     planned
├── keys/       packtrix-repo.asc, packtrix-repo.gpg
└── clients/    ubuntu-24.04.sources, rocky-9.repo (planned), README
```

On each machine, copy two files: its client file and the repository key. `clients/README` lists them for each target.

```
clients/ubuntu-24.04.sources  ->  /etc/apt/sources.list.d/packtrix.sources
keys/packtrix-repo.gpg        ->  /etc/apt/keyrings/packtrix-repo.gpg
```

```
# /etc/apt/sources.list.d/packtrix.sources   (Ubuntu 24.04)
Types: deb
URIs: http://repo.internal/apt/ubuntu-24.04/
Suites: noble
Components: main
Signed-By: /etc/apt/keyrings/packtrix-repo.gpg
```

`URIs` comes from `sync --base-url`, the URL where the published directory is served. Without it, the files use `http://packtrix-repository.invalid`, which `apt` refuses, so a missing URL can't go unnoticed. A machine without internet access should have no other source, so move the distro's own entries, such as `/etc/apt/sources.list.d/ubuntu.sources`, aside.

All packages for a target, from the distro and from vendors, are merged into one repository. Each machine therefore needs a single source entry.

## Day 2: updates

Today, `packtrix plan` writes the new lock and `git diff packtrix.lock` shows what changed. The commands marked *planned* below make it shorter:

```console
$ packtrix check                       # planned; exit 2: something changed upstream
$ packtrix plan                        # new lockfile; printing it as a diff is planned
ubuntu-24.04
  ~ openssl      3.0.13-0ubuntu3.4 → 3.0.13-0ubuntu3.5   [security]
  + libfoo1      1.2-1                                    new dependency of nginx
  - libbar0      0.9-2                                    no longer required
$ git commit packtrix.lock             # open a pull request; a reviewer approves the diff
$ packtrix sync
$ packtrix export --since <previous packtrix.lock> -o delta-2026-10-29.tar.zst   # planned

# offline side: import the delta; the previous snapshot stays available for rollback
$ packtrix import delta-2026-10-29.tar.zst --to /srv/repos   # planned
```

### Approval through Git

When `updates.publish` is `approval`, the lockfile in Git is the approval gate:

1. A scheduled CI job runs `packtrix plan`. Once `packtrix check` exists, it runs that first, and plans only if something changed.
2. The job opens a pull request with the new `packtrix.lock`.
3. A reviewer reads the diff: what changes, which repositories and keys are new.
4. Merging the pull request triggers `packtrix sync`, and `packtrix export` once it exists.

No server or operator is needed. This is the same model as dependency-update bots.

## Deployment setups

| Setup | Where Packtrix runs | How content crosses |
|---|---|---|
| **Fully air-gapped** | Connected build machine or CI | Bundle on approved media or through a data diode |
| **DMZ / semi-connected** | A host in the DMZ | Internal machines read directly from its web server |
| **GitOps** | CI pipeline | Bundles published as CI artifacts, then moved by your transfer process |
| **Kubernetes** *(later)* | A `CronJob` running the same CLI | Same as above |
