# Concepts

> **Status: design draft.**

This page defines the vocabulary used everywhere else in the docs.

## Target

A **target** is one kind of machine in your offline environment: a distribution, a release and one or more architectures. Packtrix builds one repository per target.

There are two ways to define a target:

```yaml
targets:
  # A distro Packtrix knows: its official repositories, signing keys and format are built in.
  - distro: ubuntu-24.04

  # A distro Packtrix doesn't know: list its repositories yourself.
  - name: oracle-9
    repos:
      - { name: ol9-baseos, type: rpm, url: "https://yum.oracle.com/repo/OracleLinux/OL9/baseos/latest/x86_64/", gpgKey: keys/RPM-GPG-KEY-oracle }
```

You never type an OS name, version or package format. Packtrix reads them from the repositories' own metadata: the APT `Release` file states `Origin`, `Version` and `Codename`, and an RPM distribution's BaseOS repository contains its release package.

**Dependencies are resolved within one target only.** A target must never mix releases. If Ubuntu 22.04 and 24.04 repositories were mixed, the resolver would pick the newer 24.04 builds for every machine, and they wouldn't install on 22.04. Packtrix rejects a target whose repositories disagree on distribution, release or format.

## Package

The `packages` list is written once and resolved for every target.

```yaml
packages:
  - curl                         # same name everywhere
  - name: apache                 # a label for the entry, plus the real names per format
    deb: apache2
    rpm: httpd
  - name: kubelet
    version: "1.34.*"            # version constraint
  - name: software-properties-common
    except: [debian-13]          # not available on that target
  - name: nginx
    repo: nginx-org              # take it from this vendor, not from the distro
```

- A package you list is **always** included, even if a minimal installation of the distro already contains it.
- If a listed package can't be found for a target, `plan` fails. It is never skipped silently.

## Repository

A **repository** is where Packtrix looks for packages. It is not what Packtrix downloads: only the packages you list, plus their dependencies, are downloaded. Adding a repository with 10,000 packages costs nothing if you need two of them.

There are three kinds:

| Kind | Example | How it is added |
|---|---|---|
| **Distro repositories** | Ubuntu archive, `-updates` and `-security`; Rocky BaseOS and AppStream | Automatically, from `distro` |
| **Built-in vendor repositories** | Docker, Kubernetes, NVIDIA, EPEL | Automatically, when a listed package needs one (see [auto-detection](catalog.md#auto-detection)) |
| **Custom repositories** | Your internal repository; a distro that isn't built in | By you: `extraRepos` on a known distro, or `repos` for a target defined from scratch |

**The distro always wins.** If a package exists in the distro, the distro's build is used. A vendor build is used only when you ask for it with `repo:`.

## Dependency resolution

For each target, Packtrix runs the distro's own resolver (`apt` or `dnf`) inside a throwaway container of that distro. It then records exactly what that resolver would install. See [Architecture](architecture.md#resolution).

### Baseline

A dependency closure is always relative to what the machine already has. The **baseline** sets that starting point:

| Baseline | Meaning | Use it when |
|---|---|---|
| `minimal` *(default)* | A minimal installation of the distro is assumed. What it already contains isn't downloaded unless you list it. | Machines are installed from the distro's own media |
| `none` | Nothing is assumed. Everything down to the C library is included. | The offline repository must also patch the base system |
| `lock:<file>` | The exact package set of a discovered host | You want only what one specific host is missing *(planned, see discovery)* |

### Other resolution settings

- **Recommended packages** (APT `Recommends`, RPM weak dependencies) are off by default.
- **Alternatives** (`a | b` in APT): the first alternative is used by default.

## Lockfile

`plan` writes **`packtrix.lock`**, and `sync` carries it out exactly. Commit the lockfile next to the manifest. For every target and architecture it records:

- every package: name, exact version, architecture, source repository, URL, SHA-256 and size;
- fingerprints of the upstream indexes and signing keys used;
- the version of the built-in catalog.

The manifest says what you want; the lockfile says exactly what you got. **An update is a lockfile diff,** and that diff is what a reviewer approves.

## Snapshot

Every `sync` builds a new, immutable **snapshot** of all repositories. A `current` pointer switches to it atomically, so clients never see a half-built repository. Earlier snapshots are kept for rollback (`publish.keepSnapshots`).

Package files are stored once, in a content-addressed pool shared by every snapshot and target. Keeping five snapshots doesn't take five times the space.

## Bundle

A **bundle** is a single file (`tar.zst`, optionally split into parts) that carries a snapshot across the air gap. It can be:

- **full:** everything needed to build the repositories from scratch;
- **delta:** only what changed since a previous lockfile.

Bundles are signed and include their lockfile. On the offline side, `import` verifies a bundle and adds it as a new snapshot. A bundle is a plain tar archive, so it can be unpacked even without Packtrix.
