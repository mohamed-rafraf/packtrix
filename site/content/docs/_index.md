---
title: Documentation
editURL: https://github.com/packtrix/packtrix/edit/main/docs/README.md
---

Packtrix v0.1.0 is the first release. It builds signed APT repositories for Ubuntu 22.04 and 24.04 and Debian 12 and 13; RPM, vendor repositories and bundles are planned, and each page marks what isn't implemented yet. The manifest API is `v1alpha1`: names, fields and commands may still change in later releases.

Start with Getting started, then the Overview, Concepts, the Manifest reference and the Workflow. Architecture and Security are for contributors and for anyone reviewing the trust model.

{{< cards >}}
  {{< card link="getting-started" title="Getting started" subtitle="Build offline repositories for Ubuntu and Debian, serve them, and install from them, step by step" icon="play" >}}
  {{< card link="overview" title="Overview" subtitle="Why Packtrix exists, who it is for, what it is not, and how it relates to other tools" icon="light-bulb" >}}
  {{< card link="concepts" title="Concepts" subtitle="Targets, packages, repositories, dependency resolution, baseline, lockfile, snapshots, bundles" icon="book-open" >}}
  {{< card link="manifest" title="Manifest reference" subtitle="Every manifest field, its default, and the validation rules" icon="document-text" >}}
  {{< card link="workflow" title="Workflow" subtitle="The commands, the connected and offline sides, updates and approvals" icon="refresh" >}}
  {{< card link="architecture" title="Architecture" subtitle="How resolution, download, metadata generation and publishing work" icon="chip" >}}
  {{< card link="security" title="Security" subtitle="How trust flows from upstream vendors to your machines, and how HTTPS and GPG are handled" icon="shield-check" >}}
  {{< card link="catalog" title="Built-in catalog" subtitle="Which distros and vendor repositories are built in, and how Packtrix picks them" icon="collection" >}}
  {{< card link="roadmap" title="Roadmap" subtitle="Delivery phases and decisions that are still open" icon="map" >}}
  {{< card link="releasing" title="Releasing" subtitle="How releases are cut, and how to verify one before installing it" icon="badge-check" >}}
{{< /cards >}}
