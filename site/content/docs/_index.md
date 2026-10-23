---
title: Documentation
editURL: https://github.com/packtrix/packtrix/edit/main/docs/README.md
---

Packtrix is in early development. These pages describe what it will do and how. So far the manifest format and `packtrix validate` are implemented; names, fields and commands may change.

Start with the Overview, then Concepts, the Manifest reference and the Workflow. Architecture and Security are for contributors and for anyone reviewing the trust model.

{{< cards >}}
  {{< card link="overview" title="Overview" subtitle="Why Packtrix exists, who it is for, what it is not, and how it relates to other tools" icon="light-bulb" >}}
  {{< card link="concepts" title="Concepts" subtitle="Targets, packages, repositories, dependency resolution, baseline, lockfile, snapshots, bundles" icon="book-open" >}}
  {{< card link="manifest" title="Manifest reference" subtitle="Every manifest field, its default, and the validation rules" icon="document-text" >}}
  {{< card link="workflow" title="Workflow" subtitle="The commands, the connected and offline sides, updates and approvals" icon="refresh" >}}
  {{< card link="architecture" title="Architecture" subtitle="How resolution, download, metadata generation and publishing work" icon="chip" >}}
  {{< card link="security" title="Security" subtitle="How trust flows from upstream vendors to your machines, and how HTTPS and GPG are handled" icon="shield-check" >}}
  {{< card link="catalog" title="Built-in catalog" subtitle="Which distros and vendor repositories are built in, and how Packtrix picks them" icon="collection" >}}
  {{< card link="roadmap" title="Roadmap" subtitle="Delivery phases and decisions that are still open" icon="map" >}}
{{< /cards >}}
