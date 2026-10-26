# Releasing

Packtrix sits on the supply chain of the machines it serves, so every release can be verified: who built it, from which commit, and that nothing changed since.

## What a release contains

The [release workflow](../.github/workflows/release.yaml) runs [GoReleaser](../.goreleaser.yaml) for each `v*` tag, and publishes a GitHub release with:

| File | What it is |
|---|---|
| `packtrix_<version>_<os>_<arch>.tar.gz` | The static binary for linux and darwin, amd64 and arm64, with `LICENSE` and `README.md` |
| `….tar.gz.sbom.json` | The SBOM of each archive, in SPDX, written by syft |
| `checksums.txt` | The SHA-256 of every archive and SBOM |
| `checksums.txt.sigstore.json` | A keyless Sigstore signature of `checksums.txt`, made with the identity of the release workflow |

The workflow also attests the build provenance of every file in `checksums.txt`, with GitHub's artifact attestations. Release notes group the merged pull requests by the type of their title: features, fixes, documentation, and the rest.

## Cutting a release

1. Check that `main` is ready:
   - CI is green, including the nightly `e2e` and `compare` workflows;
   - the release's milestone has no open issues;
   - the docs describe what the release does.
2. Try the release locally. `make release-snapshot` builds everything into `dist/`, without signing or publishing.
3. Tag the commit on `main`, and push the tag:

   ```console
   $ git switch main && git pull
   $ git tag -a v0.1.0 -m "v0.1.0"
   $ git push origin v0.1.0
   ```

   A tag with a suffix, such as `v0.2.0-rc.1`, makes a pre-release.
4. Watch the `release` workflow. When it's done, verify the release as below, then edit the release notes on GitHub if needed.

If the workflow fails, fix the cause on `main`. Then delete the tag, and the GitHub release if the workflow created one, and tag again.

## Verifying a release

Download the archive for your platform, with `checksums.txt` and its signature:

```console
$ gh release download v0.1.0 --repo packtrix/packtrix \
    --pattern 'packtrix_*_linux_amd64.tar.gz' --pattern 'checksums.txt*'
```

**1. The signature of the checksums**, with [cosign](https://docs.sigstore.dev/cosign/system_config/installation/). It proves that the release workflow of `packtrix/packtrix` signed this `checksums.txt`, when building this tag:

```console
$ cosign verify-blob checksums.txt \
    --bundle checksums.txt.sigstore.json \
    --certificate-identity https://github.com/packtrix/packtrix/.github/workflows/release.yaml@refs/tags/v0.1.0 \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com
Verified OK
```

**2. The archive against the checksums:**

```console
$ sha256sum --check --ignore-missing checksums.txt
packtrix_0.1.0_linux_amd64.tar.gz: OK
```

**3. The build provenance**, with the GitHub CLI. It shows the repository, workflow and commit that built the archive:

```console
$ gh attestation verify packtrix_0.1.0_linux_amd64.tar.gz --repo packtrix/packtrix
```

Then install the binary, and check its version:

```console
$ tar -xzf packtrix_0.1.0_linux_amd64.tar.gz packtrix
$ sudo install packtrix /usr/local/bin/
$ packtrix version
```
