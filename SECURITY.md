# Security policy

Packtrix sits on the supply chain of the machines it serves. A flaw in how it verifies upstream content, signs repositories or handles credentials can reach every one of them, so please report one privately.

## Supported versions

Packtrix is in early development. Security fixes go into the next release, and only the latest release is supported: upgrade to it to get them.

## Reporting a vulnerability

Report it through GitHub's private vulnerability reporting: **[Report a vulnerability](https://github.com/packtrix/packtrix/security/advisories/new)**, on the repository's Security tab. Please don't open a public issue, pull request or discussion about it.

Include what you can:

- the version (`packtrix version`) and platform;
- what an attacker can do, and what they need for it: a position on the network, a malicious mirror or repository, write access to the manifest, …;
- the steps, or a manifest, to reproduce it.

## What to expect

- An acknowledgement within a week.
- An assessment, and the plan for a fix, in the private advisory, where you can follow and comment on the work.
- A fix in a new release, then a published security advisory, with a CVE when it applies. It credits you, unless you'd rather not be named.

Please keep the details private until the fix is released. The release date is agreed with you.

## Scope

In scope: the `packtrix` binary, and the repositories and client files it publishes. For example:

- accepting upstream content that isn't signed by a pinned key, or whose hashes don't match;
- publishing something that the lock doesn't list, in a repository that the machines trust;
- leaking credentials or the signing key, in output, files or requests;
- writing outside the publish path, from upstream metadata or a manifest.

Out of scope: vulnerabilities in the packages that Packtrix mirrors, and in `apt` or `dnf` on the machines. Report those to the distribution or vendor. [docs/security.md](docs/security.md) describes the trust model.
