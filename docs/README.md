# Packtrix documentation

> **Status: early development.** Packtrix builds signed APT repositories for Ubuntu 22.04 and 24.04 and Debian 12 and 13: [Getting started](getting-started.md) walks through it. RPM, vendor repositories and bundles are planned; each page marks what isn't implemented yet. Names, fields and commands may change before the first release.

| Document | Read it to learn |
|---|---|
| [Getting started](getting-started.md) | Build offline repositories for Ubuntu and Debian, serve them, and install from them, step by step |
| [Overview](overview.md) | Why Packtrix exists, who it is for, what it is not, and how it relates to other tools |
| [Concepts](concepts.md) | The vocabulary: targets, packages, repositories, dependency resolution, baseline, lockfile, snapshots, bundles |
| [Manifest reference](manifest.md) | Every manifest field, its default, and the validation rules |
| [Workflow](workflow.md) | The commands, the connected and offline sides, updates and approvals |
| [Architecture](architecture.md) | How resolution, download, metadata generation and publishing work |
| [Security](security.md) | How trust flows from upstream vendors to your machines, and how HTTPS and GPG are handled |
| [Built-in catalog](catalog.md) | Which distros and vendor repositories are built in, and how Packtrix picks them automatically |
| [Roadmap](roadmap.md) | Delivery phases and decisions that are still open |
| [Releasing](releasing.md) | How releases are cut, and how to verify one before installing it |

**Suggested reading order:** Getting started → Overview → Concepts → Manifest reference → Workflow. Architecture and Security are for contributors and for anyone reviewing the trust model.
