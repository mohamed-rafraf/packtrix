# Contributing to Packtrix

Thanks for your interest. Packtrix is in its **design phase**: there is no code yet, and the most valuable contributions are about the design itself.

## What helps most right now

- **Design feedback.** If something in [`docs/`](docs/) is unclear, wrong or missing, open an issue.
- **Real-world requirements.** The package lists, distro combinations and air-gap transfer constraints you work with (media size limits, approval processes, scanning). These shape the manifest and the defaults.
- **Documentation fixes.** Pull requests for typos, clarity and examples are welcome.

For larger design changes, open an issue first so we can agree on the direction before you write a pull request.

## How changes are made

`main` is protected:

- Every change goes through a **pull request**. Direct pushes, force pushes and branch deletion are blocked.
- Pull requests are **squash-merged**, so each one becomes a single commit on `main`.
- All review conversations must be resolved before merging.

### Pull request titles

The pull request title becomes the commit message on `main`. Use a short, imperative title with a [Conventional Commits](https://www.conventionalcommits.org/) prefix:

```
docs: explain baseline in the concepts page
feat: add rocky-10 to the catalog
fix: reject targets that mix releases
```

Common prefixes: `docs`, `feat`, `fix`, `refactor`, `test`, `ci`, `chore`.

### Branches

Name branches after the change, e.g. `docs/baseline-explanation` or `feat/catalog-rocky-10`. Branches are deleted automatically after merging.

## License

By contributing, you agree that your contributions are licensed under the [Apache License 2.0](LICENSE).

Every Go, shell, YAML and Makefile file starts with the license header from [`hack/boilerplate/`](hack/boilerplate/), with `YEAR` replaced by the year the file was created. Pull requests are checked automatically; run `make verify-boilerplate` to check locally.
