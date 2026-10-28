<h1 align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo/packtrix-logo-dark.svg">
    <img alt="Packtrix" src="docs/assets/logo/packtrix-logo.svg" width="360">
  </picture>
</h1>

<p align="center"><strong>Declarative offline APT and RPM repositories.</strong></p>

<p align="center"><a href="https://scorecard.dev/viewer/?uri=github.com/packtrix/packtrix"><img alt="OpenSSF Scorecard" src="https://api.scorecard.dev/projects/github.com/packtrix/packtrix/badge"></a></p>

List the packages your air-gapped Linux machines need. Packtrix resolves every dependency for every distro you run, then builds signed APT and RPM repositories you can carry across the air gap.

> [!NOTE]
> **Status: v0.1.0, the first release.** Packtrix builds signed APT repositories for Ubuntu 22.04 and 24.04 and Debian 12 and 13, on amd64 and arm64: `packtrix plan` resolves every target against the verified upstream metadata into `packtrix.lock`, and `packtrix sync` downloads, builds and publishes the repositories with the machines' client files. RPM distributions, vendor repositories and bundles for the air gap are next; see the [roadmap](docs/roadmap.md). The manifest API is `v1alpha1`: fields and commands may still change in later releases. Feedback is welcome; see [Contributing](#contributing).

## Why

Machines without internet access still need packages. Today that usually means one of three things:

- Mirroring entire distributions (hundreds of GB per release) just to install a few dozen packages.
- Hand-written `apt-get download` or `dnf download --resolve` scripts, one per distro, that break on the next release.
- Repository platforms that were built for connected networks or need a full server stack to run.

Packtrix takes the distros you run and the packages you need, and produces exactly that: those packages plus all their dependencies, as standard repositories that `apt` and `dnf` use with no extra client.

## Example

```yaml
apiVersion: packtrix.io/v1alpha1
kind: OfflineRepository
metadata:
  name: k8s-nodes
spec:
  targets:
    - distro: ubuntu-24.04
    - distro: ubuntu-22.04
    - distro: debian-12

  packages:
    - curl
    - jq
    - conntrack
    - socat
    - nfs-common

  publish:
    signing:
      key: env:PACKTRIX_SIGNING_KEY   # your key signs the repositories
```

```console
$ packtrix plan                                   # resolve dependencies for each distro, write packtrix.lock
$ packtrix sync --base-url http://repo.internal   # download, verify, build signed repositories
```

The result is one APT repository per distro, with a ready-made source entry for each, to serve with any web server. Coming next: RPM distros such as `rocky-9` in the same manifest, vendor repositories added when a package needs one (`docker-ce`, `kubelet`), and `packtrix export`, one file to carry across the air gap.

## Get started

Download the binary for your platform from the [releases](https://github.com/packtrix/packtrix/releases), for Linux or macOS, amd64 or arm64:

```console
$ VERSION=0.1.0 OS=linux ARCH=amd64
$ curl -fsSLO https://github.com/packtrix/packtrix/releases/download/v$VERSION/packtrix_${VERSION}_${OS}_${ARCH}.tar.gz
$ tar -xzf packtrix_${VERSION}_${OS}_${ARCH}.tar.gz packtrix
$ sudo install packtrix /usr/local/bin/
```

Every release is signed, and comes with checksums, SBOMs and build provenance: [Releasing](docs/releasing.md#verifying-a-release) shows how to check them. With Go 1.26 or newer, `go install github.com/packtrix/packtrix/cmd/packtrix@latest` builds it instead.

[Getting started](docs/getting-started.md) goes from a signing key to `apt-get install` on a machine, in about ten minutes.

## How it works

```
        CONNECTED SIDE                                        OFFLINE SIDE
┌────────────────────────────────────────┐            ┌─────────────────────────────┐
│  manifest ─► plan ─► sync ─► export    │   bundle   │  import ─► serve            │
│               │                        │  ────────► │                             │
│               ▼                        │  USB, data │  apt/ and rpm/ repositories │
│     packtrix.lock (exact versions,     │  diode, …  │  one URL, one signing key   │
│     checksums, sources)                │            │                             │
└────────────────────────────────────────┘            └─────────────────────────────┘
```

- **One manifest for APT and RPM.** You name the distros, and Packtrix already knows their official repositories and signing keys.
- **Dependency resolution built in.** Packtrix resolves dependencies from the repositories' own metadata, following the rules `apt` and `dnf` apply. It's a single binary: no container runtime, no `apt` or `dnf` needed.
- **Vendor repositories are added automatically** *(planned)*. Docker, Kubernetes, NVIDIA, EPEL and others are added when a package needs them, and the plan always shows it.
- **Every change is reviewable.** A lockfile records exactly what entered the air gap. Updates show up as a diff of it, and *(planned)* ship as delta bundles.
- **Plain output.** The result is standard, signed APT and RPM repositories, with no agent or plugin on the machines.

## Use cases

- **Air-gapped Kubernetes nodes:** prerequisites, container runtime and kubelet for Ubuntu and RHEL-family nodes.
- **Regulated and sovereign networks:** approved package sets, with every change reviewed before it enters.
- **Offline security patching:** scheduled, security-only delta bundles.
- **Edge and remote sites:** small repositories and small updates over thin links.
- **Mixed fleets and migrations:** moving from Ubuntu 22.04 to 24.04, or from CentOS to Rocky, means adding a target. The package list stays the same.

Packtrix doesn't mirror whole distributions, cache packages for connected networks, build packages, or handle container images. See [what Packtrix is not](docs/overview.md#what-packtrix-is-not).

## Documentation

The documentation is also on the website: **[packtrix.github.io](https://packtrix.github.io/)**.

| Document | Contents |
|---|---|
| [Getting started](docs/getting-started.md) | Build, serve and use offline repositories for Ubuntu and Debian, step by step |
| [Overview](docs/overview.md) | The problem, goals, use cases, related tools |
| [Concepts](docs/concepts.md) | Targets, packages, repositories, resolution, lockfile, snapshots, bundles |
| [Manifest reference](docs/manifest.md) | Every field of the manifest (draft) |
| [Workflow](docs/workflow.md) | How Packtrix is used day to day, including updates |
| [Architecture](docs/architecture.md) | How it works inside |
| [Security](docs/security.md) | Trust model, GPG signing, HTTPS |
| [Built-in catalog](docs/catalog.md) | Built-in repositories and how they are detected automatically |
| [Roadmap](docs/roadmap.md) | Phases and open questions |
| [Releasing](docs/releasing.md) | How releases are cut and verified |

More examples are in [`examples/`](examples/).

## Contributing

The project is in early development: APT works, RPM is next. The most useful contributions right now:

- **Try it** with your package lists, and open an issue for anything that doesn't resolve or install.
- **Feedback on the design.** Open an issue if something is unclear, wrong or missing.
- **Real-world requirements.** Package lists, distro combinations and air-gap transfer constraints you deal with.

`main` is protected: every change goes through a pull request and is squash-merged. See [CONTRIBUTING.md](CONTRIBUTING.md). Everyone taking part follows the [code of conduct](CODE_OF_CONDUCT.md).

## License

[Apache License 2.0](LICENSE)
