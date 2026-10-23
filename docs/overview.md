# Overview

## The problem

Many Linux machines run without internet access: defense and public-sector networks, banks, critical infrastructure, factories, ships, labs. They still need packages, both to install software and to receive security fixes.

The usual options are all costly:

| Approach | Cost |
|---|---|
| **Mirror entire distributions** | Hundreds of GB per release and architecture, most of which is never used. In regulated networks every file that crosses the air gap must be scanned and approved, so size matters. |
| **Scripts per distro** (`apt-get download`, `dnf download --resolve`) | One script per distro and release. They miss dependencies that happen to be installed on the preparing machine, don't sign anything, and don't handle updates. |
| **Repository platforms** (Pulp/Katello, Nexus, Artifactory, …) | Built for connected networks, or a server stack to deploy and operate just to produce a few repositories. |

Mixed fleets double the work: Ubuntu and RHEL-family machines need two toolchains, two sets of scripts and two sets of keys.

## What Packtrix does

**Input:** the distros your machines run, and the packages they need.

**Output:** for every distro, a signed repository containing those packages and all their dependencies, ready to carry across the air gap and serve with any web server.

```yaml
spec:
  targets:
    - distro: ubuntu-24.04
    - distro: rocky-9
  packages: [curl, jq, docker-ce, nfs-common]
```

### Design principles

1. **One manifest for APT and RPM.** The package list is written once. Differences between formats are per-package exceptions, not separate sections.
2. **Built-in resolution, checked against `apt` and `dnf`.** Packtrix resolves dependencies itself, from the repositories' metadata, following the rules `apt` and `dnf` apply. It needs no container runtime, and its results are checked against the real tools.
3. **Complete or nothing.** If a package or one of its dependencies can't be resolved for a target, `plan` fails and names the package, the target and the reason. A partial repository is never published.
4. **Everything that crosses the air gap is recorded.** The lockfile lists every package, version, checksum and source.
5. **Standard output.** Plain APT and RPM repositories. Machines need no agent, plugin or special client.
6. **Light offline side.** The connected side resolves and downloads. The offline side only verifies, stores and serves, using a single binary or any web server.

## Who it is for

- **Platform and infrastructure teams** who run Linux machines in offline or air-gapped environments.
- **Security teams** who must approve what enters those environments.
- **Teams building offline installers**, such as Kubernetes distributions, appliances or on-premises products, who need their OS-level prerequisites bundled.

## Use cases

1. **Air-gapped Kubernetes nodes.** Node prerequisites (`conntrack`, `socat`, `ipvsadm`, storage clients), the container runtime and `kubelet`/`kubeadm`, for both Ubuntu and RHEL-family nodes, from one manifest.
2. **Regulated and sovereign networks.** An approved package set where every change is a reviewed diff. The lockfile serves as the audit trail of what entered the network.
3. **Offline security patching.** A scheduled job picks up security updates only and ships them as a small delta bundle.
4. **Edge and remote sites.** Small repositories and small updates for sites with slow or intermittent links.
5. **Mixed fleets and migrations.** Moving from Ubuntu 22.04 to 24.04, or from CentOS to Rocky, means adding a target. The package list stays the same.
6. **Reproducible image builds.** Packer or CI image builds install from a pinned snapshot instead of from whatever upstream serves that day.
7. **Rebuilding a host** *(planned).* Inspect an existing server and generate the manifest that reproduces its packages.

## What Packtrix is not

| Not this | Use instead |
|---|---|
| A full mirror of a distribution | `apt-mirror`, `debmirror`, `reposync` |
| A caching proxy for connected networks | Nexus, Artifactory, a pull-through cache |
| A tool to build your own `.deb`/`.rpm` packages | nFPM, fpm, distro build tooling |
| A transport for container images or Helm charts | Zarf, Hauler, registry mirroring |
| Configuration management | Ansible, Salt, Puppet |

Packtrix can work alongside these tools. A Packtrix bundle can travel next to container images in the same air-gap transfer.

## Related tools

| Tool | Focus | How Packtrix differs |
|---|---|---|
| **aptly** | APT repository management: mirrors with dependency filtering, snapshots, publishing | Packtrix covers APT and RPM from one manifest |
| **Pulp / Katello / Red Hat Satellite** | Content management platforms for RPM and Debian content | Packtrix is a single CLI with no server stack, focused on producing offline repositories |
| **Nexus / Artifactory** | Hosting and proxying many package formats | Proxies cache what clients request. Packtrix computes the complete set *before* the transfer. |
| **Zarf / Hauler** | Air-gap delivery of Kubernetes content: images, charts, files | They don't resolve OS package dependencies. The two complement each other. |
