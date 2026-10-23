<h1 align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo/packtrix-logo-dark.svg">
    <img alt="Packtrix" src="docs/assets/logo/packtrix-logo.svg" width="360">
  </picture>
</h1>

<p align="center"><strong>Declarative offline APT and RPM repositories.</strong></p>

List the packages your air-gapped Linux machines need. Packtrix resolves every dependency for every distro you run, then builds signed APT and RPM repositories you can carry across the air gap.

> [!NOTE]
> **Status: early development.** The CLI can validate manifests (`packtrix validate`). Resolving, downloading and publishing aren't implemented yet. The manifest format, CLI and architecture described here are proposals that will change. Feedback is welcome; see [Contributing](#contributing).

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
    - distro: rocky-9

  packages:
    - curl
    - jq
    - conntrack
    - docker-ce                # Docker's repository is added automatically
    - name: kubelet
      version: "1.34.*"        # picks the Kubernetes 1.34 repository
    - name: apache
      deb: apache2             # the name differs between deb and rpm
      rpm: httpd
```

```console
$ packtrix plan                       # resolve dependencies for each distro, show what will be downloaded
$ packtrix sync                       # download, verify and build signed repositories
$ packtrix export -o bundle.tar.zst   # one file to carry across the air gap
```

### Try it

What works today is checking a manifest:

```console
$ go install github.com/packtrix/packtrix/cmd/packtrix@latest
$ packtrix validate -f examples/mixed-fleet.yaml
examples/mixed-fleet.yaml is valid: 4 target(s), 11 package(s)
$ packtrix validate -f examples/mixed-fleet.yaml -o yaml   # print it with every default filled in
```

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
- **Vendor repositories are added automatically.** Docker, Kubernetes, NVIDIA, EPEL and others are added when a package needs them, and the plan always shows it.
- **Every change is reviewable.** A lockfile records exactly what entered the air gap. Updates show up as a diff and ship as delta bundles.
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
| [Overview](docs/overview.md) | The problem, goals, use cases, related tools |
| [Concepts](docs/concepts.md) | Targets, packages, repositories, resolution, lockfile, snapshots, bundles |
| [Manifest reference](docs/manifest.md) | Every field of the manifest (draft) |
| [Workflow](docs/workflow.md) | How Packtrix is used day to day, including updates |
| [Architecture](docs/architecture.md) | How it works inside |
| [Security](docs/security.md) | Trust model, GPG signing, HTTPS |
| [Built-in catalog](docs/catalog.md) | Built-in repositories and how they are detected automatically |
| [Roadmap](docs/roadmap.md) | Phases and open questions |

More examples are in [`examples/`](examples/).

## Contributing

The project is in its design phase. The most useful contributions right now:

- **Feedback on the design.** Open an issue if something is unclear, wrong or missing.
- **Real-world requirements.** Package lists, distro combinations and air-gap transfer constraints you deal with.

`main` is protected: every change goes through a pull request and is squash-merged. See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[Apache License 2.0](LICENSE)
