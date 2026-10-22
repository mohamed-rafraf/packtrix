# Security

> **Status: design draft.**

Packtrix sits on the supply chain of every machine it serves. This page describes how trust flows from upstream vendors to those machines, and what Packtrix asserts at each step.

## Principles

- **Verify everything at the gate.** No package enters the pool unless it traces back to an upstream signature made by a pinned key.
- **Never trust a key fetched at runtime.** Keys come from the built-in catalog or your manifest and are pinned by fingerprint.
- **Make trust changes visible.** A new repository or a new signing key appears in `plan` and in the lockfile diff, so a reviewer sees it before it takes effect.
- **A checksum is not authenticity.** A matching SHA-256 proves a file is the one the index describes. Only the signature on that index proves the index came from the vendor.

## Verifying upstream content

| Format | How the trust chain works upstream | What Packtrix verifies |
|---|---|---|
| **APT** | The signed `InRelease` file lists hashes of the `Packages` indexes, which list hashes of each `.deb`. Individual `.deb` files are not signed. | The `InRelease` signature against the pinned key, then each index hash, then each `.deb` hash |
| **RPM** | Each `.rpm` carries an embedded GPG signature. Most distributions don't sign `repomd.xml`. | Each package's signature against the pinned key, then each file hash against the metadata |

If any check fails, `sync` stops and nothing is published.

## Signing the published repositories

This is where APT and RPM differ, and it matters for what your machines trust:

| Format | What happens | What machines trust |
|---|---|---|
| **APT** | Packtrix publishes a subset of the upstream suite, so the upstream `InRelease` signature no longer matches. Packtrix writes a new `InRelease` and **signs it with your key**. | Your key. Machines trust Packtrix's output, which Packtrix built only from upstream-verified content. |
| **RPM** | Package signatures are embedded in the files and survive unchanged. Packtrix also signs `repomd.xml` with your key. | **The vendor's keys**, end to end, with `gpgcheck=1`. Your key is used only for the metadata (`repo_gpgcheck=1`). |

The client files Packtrix generates set this up correctly for each format.

### Your signing key

- Referenced from the manifest (`publish.signing.key`), never embedded in it: `env:VAR` or `file:/path`. Support for key management services is planned.
- Signing is done in-process (Go OpenPGP), with no `gpg` binary or agent required.
- Use RSA-3072 or RSA-4096 with SHA-256 or stronger digests. RHEL 9 rejects SHA-1 signatures, and current `apt` warns about weak keys.
- **Rotation:** machines pin your key with `Signed-By`/`gpgkey`. To rotate, publish signatures from both the old and new keys during an overlap period, update the client files, then retire the old key.
- **Expiry:** Packtrix doesn't set `Valid-Until` on the `Release` files it publishes. An offline repository that "expires" after a week would break machines that can't fetch a new one. Upstream `Valid-Until` values are ignored for content that has already been verified at sync time.

## Built-in catalog trust

Shipping repository definitions and keys makes the built-in catalog a trust anchor. Rules:

- Only official vendor repositories are included, with keys whose fingerprints match the vendor's own documentation.
- Every catalog change that adds or changes a key is reviewed against that documentation.
- Catalog releases are versioned and signed. The version used is recorded in the lockfile, so a catalog update never silently changes an existing lockfile.
- `packtrix catalog show <name>` and `packtrix render` print exactly which URLs and key fingerprints will be used.
- `autoRepos.deny` lets an organization forbid repositories it doesn't accept, e.g. EPEL.

## Bundles

- Every bundle has a signed manifest with the SHA-256 of each file.
- `import` refuses bundles with a bad signature, a missing file or a hash mismatch, and refuses deltas whose base snapshot doesn't match the offline side.

## HTTPS

### Fetching from upstream

- APT security doesn't depend on transport, because the signatures above protect the content. Plain HTTP upstreams such as the Ubuntu archive are fine.
- Packtrix supports a custom CA bundle and HTTP(S) proxies, for networks that intercept TLS or only reach the internet through a proxy.
- **Authenticated upstreams** (RHEL subscription certificates, tokens, basic auth) are configured with `auth`, as references to environment variables or files, never as literal values in the manifest.

### Serving on the offline side

- **Default: plain HTTP plus GPG.** This is the standard Debian model. Content integrity and authenticity come from the signatures, not from TLS.
- **HTTPS is optional** with a certificate you provide. Air-gapped networks can't use public ACME CAs, so the certificate usually comes from an internal CA.
- **Bootstrap:** machines need the repository key (and, for HTTPS, the internal CA certificate) before they can use the repository. The generated `clients/` and `keys/` directories contain everything to copy onto a machine. Distributing those files belongs to your provisioning process (image build, cloud-init, configuration management).
- For production, serving `/srv/repos/current` with nginx, Apache or Caddy is recommended. `packtrix serve` is meant for small setups and testing.

## Reporting a vulnerability

There is no code yet. Once there is, a `SECURITY.md` with a private reporting channel will be added before the first release.
