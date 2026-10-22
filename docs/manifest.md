# Manifest reference

> **Status: draft of `packtrix.dev/v1alpha1`.** Fields and defaults may change before the first release.

Packtrix reads `packtrix.yaml` from the current directory by default. Use `-f <file>` to choose another file.

## Minimal manifest

```yaml
apiVersion: packtrix.dev/v1alpha1
kind: OfflineRepository
metadata:
  name: datacenter
spec:
  targets:
    - distro: ubuntu-24.04
    - distro: rocky-9
  packages: [curl, jq, nfs-common]
```

Everything else has a default.

## Full structure

```yaml
apiVersion: packtrix.dev/v1alpha1
kind: OfflineRepository
metadata:
  name: datacenter
spec:
  targets:                                   # which machines (required)
    - name: ubuntu-gpu                       # default: the distro name
      distro: ubuntu-24.04                   # built-in distro …
      architectures: [amd64, arm64]          # default: [amd64]
      extraRepos: []                         # … plus your own repositories
    - name: oracle-9
      repos: []                              # … or a distro defined from scratch

  packages: []                               # what the machines need (required)

  autoRepos:                                 # automatic vendor repositories
    enabled: true
    deny: []

  resolve:                                   # how dependencies are resolved
    baseline: minimal
    recommends: false
    alternatives: first
    exclude: []

  updates:                                   # how updates are picked up
    check: ""
    accept: all
    minAge: 0s
    publish: approval

  publish:                                   # where and how repositories are written
    path: ./repos
    signing: {}
    keepSnapshots: 3

  export:                                    # how bundles are written
    format: tar.zst
    splitSize: ""
```

## `spec.targets`

A list of targets. A target is either a short form `- distro: <name>` or an object:

| Field | Type | Default | Description |
|---|---|---|---|
| `name` | string | value of `distro` | Unique name. Used in `only`/`except` and in output paths. |
| `distro` | string or object | — | A built-in distro (see [catalog](catalog.md#built-in-distros)). Exactly one of `distro` or `repos` is required. |
| `architectures` | list | `[amd64]` | Normalized names: `amd64`, `arm64`. Rendered as `x86_64`/`aarch64` for RPM. |
| `extraRepos` | list of [repository](#repository-object) | `[]` | Your repositories added on top of the distro, e.g. an internal repository. |
| `repos` | list of [repository](#repository-object) | — | The complete repository list for a distro Packtrix doesn't know. Vendor repositories aren't detected automatically for these targets. |

### `distro` as an object

Use the object form only to change a distro's defaults:

```yaml
distro:
  name: rocky-9
  components: [baseos, appstream, crb]
  mirror: https://nexus.internal/repository/rocky
  auth: { … }
```

| Field | Default | Description |
|---|---|---|
| `name` | — | Built-in distro name |
| `components` | per distro | Parts of the distro to include. The same field works for every distro family. See [components](catalog.md#components). |
| `mirror` | official servers | Fetch through a mirror or proxy (Nexus, Artifactory, a local mirror) instead of the official servers. Keys and paths stay the same. |
| `auth` | — | Credentials for distros that need them, e.g. RHEL subscription certificates |

### Repository object

Used in `extraRepos` and `repos`:

| Field | Required | Description |
|---|---|---|
| `name` | yes | Unique name |
| `type` | yes | `apt` or `rpm` |
| `url` | yes | Base URL. `{arch}` is replaced with the native architecture name. |
| `suite` | apt only | e.g. `stable`, `noble`, or `/` for a flat repository |
| `components` | apt only | e.g. `[main]`. Not used for flat repositories. |
| `gpgKey` | yes | Path to the repository's public signing key |
| `auth` | no | Credentials (basic auth, bearer token, client certificate), as references to environment variables or files, never as literal values |

```yaml
extraRepos:
  - name: internal-tools
    type: apt
    url: https://pkgs.internal/apt
    suite: stable
    components: [main]
    gpgKey: keys/internal.gpg
```

## `spec.packages`

A list of packages, written once for all targets. An entry is either a string (the package name) or an object:

| Field | Description |
|---|---|
| `name` | Package name. When `deb`/`rpm` are set, it is only a label. |
| `version` | Version constraint (see below). Also selects the repository for vendors that publish one repository per version, such as Kubernetes. |
| `deb` / `rpm` | The real package name for that format, when it differs |
| `only` | Include this package only for these targets. Values are target names, or `deb`/`rpm` for a whole format. |
| `except` | Include it for every target except these. Cannot be combined with `only`. |
| `repo` | Take the package from this built-in vendor repository instead of the distro, e.g. `nginx-org` |

### Version constraints

| Constraint | Matches |
|---|---|
| *(none)* | Newest available |
| `"1.34.1"` | Upstream version 1.34.1, any packaging revision |
| `"1.34.*"` | Any 1.34.x |
| `"1.34.1-1.1"` | That exact package version |

Versions are compared with each format's native rules (`dpkg` ordering for deb; `rpm` ordering, including epochs, for rpm).

## `spec.autoRepos`

| Field | Default | Description |
|---|---|---|
| `enabled` | `true` | Add built-in vendor repositories automatically when a package needs one. See [auto-detection](catalog.md#auto-detection). |
| `deny` | `[]` | Repositories that must never be added automatically, as glob patterns, e.g. `[epel-*]` |

## `spec.resolve`

| Field | Default | Description |
|---|---|---|
| `baseline` | `minimal` | `minimal`, `none`, or `lock:<path>`. See [baseline](concepts.md#baseline). |
| `recommends` | `false` | Include APT `Recommends` and RPM weak dependencies |
| `alternatives` | `first` | For APT `a \| b` dependencies: `first` or `all` |
| `exclude` | `[]` | Packages never to download. Exact semantics are an [open question](roadmap.md#open-questions). |

## `spec.updates`

| Field | Default | Description |
|---|---|---|
| `check` | *(empty: manual)* | Cron expression for update checks when Packtrix runs as a scheduled job |
| `accept` | `all` | `all`, `security` (security updates only), or `none` (keep the lockfile pinned) |
| `minAge` | `0s` | Wait this long after a new version is published before taking it, e.g. `72h` |
| `publish` | `approval` | `auto`: publish after validation. `approval`: stop after `plan` until the lockfile change is approved. |

## `spec.publish`

| Field | Default | Description |
|---|---|---|
| `path` | `./repos` | Output directory |
| `signing.key` | — | Reference to the private signing key, e.g. `env:PACKTRIX_SIGNING_KEY` or `file:/path`. Required to publish APT repositories. |
| `keepSnapshots` | `3` | Number of earlier snapshots to keep for rollback |

## `spec.export`

| Field | Default | Description |
|---|---|---|
| `format` | `tar.zst` | Bundle format |
| `splitSize` | *(none)* | Split bundles into parts of this size, e.g. `4GiB` for FAT32 media or transfer-system limits |

## Validation rules

`packtrix validate` and `packtrix plan` reject a manifest when:

- a target has both `distro` and `repos`, or neither;
- two targets have the same name;
- `only` or `except` names an unknown target;
- an entry has both `only` and `except`;
- a target's repositories disagree on distribution, release or format;
- a listed package isn't available for a target it applies to;
- a package from a versioned vendor repository (e.g. Kubernetes) has no `version`;
- a package matches several unrelated vendor repositories and has no `repo`.

Every error names the target, the package and what to change.

## Examples

- [`examples/k8s-node-prereqs.yaml`](../examples/k8s-node-prereqs.yaml): Kubernetes node prerequisites for Ubuntu 24.04, Ubuntu 22.04 and Debian 12
- [`examples/mixed-fleet.yaml`](../examples/mixed-fleet.yaml): Ubuntu, Rocky and RHEL with Docker, Kubernetes and NVIDIA packages
- [`examples/custom-repos.yaml`](../examples/custom-repos.yaml): an internal repository and a distro defined from scratch
