# Contributing to Packtrix

Thanks for your interest. Packtrix is in **early development**: the manifest API and `packtrix validate` exist, and the rest is still being designed in [`docs/`](docs/). Contributions to the design are the most valuable right now.

## What helps most right now

- **Design feedback.** If something in [`docs/`](docs/) is unclear, wrong or missing, open an issue.
- **Real-world requirements.** The package lists, distro combinations and air-gap transfer constraints you work with (media size limits, approval processes, scanning). These shape the manifest and the defaults.
- **Documentation fixes.** Pull requests for typos, clarity and examples are welcome.

For larger design changes, open an issue first so we can agree on the direction before you write a pull request.

## Development

You need Go 1.26 or newer.

```console
$ make build                        # build bin/packtrix; GOOS=linux GOARCH=arm64 to cross-compile
$ go test ./...                     # run the tests
$ make lint                         # golangci-lint, configured in .golangci.yml
$ make update-codegen               # regenerate zz_generated.*.go after changing API types
```

The code is organized as follows:

| Path | Contents |
|---|---|
| `cmd/packtrix/` | The binary's entry point |
| `internal/cli/` | The commands |
| `pkg/apis/packtrix/` | The internal manifest API, which the rest of Packtrix uses |
| `pkg/apis/packtrix/v1alpha1/` | The `v1alpha1` manifest format: types, defaults, conversion to the internal API |
| `pkg/apis/packtrix/scheme/` | Registers every API version |
| `pkg/apis/packtrix/validation/` | Validation of the internal API |
| `pkg/apis/packtrix/manifest/` | Reading and writing manifest files |

A change to the manifest format changes the types in both `pkg/apis/packtrix/` and `v1alpha1/`. Defaults go in `v1alpha1/defaults.go` as `SetDefaults_<Type>` functions. Then run `make update-codegen` and commit the generated files; pull requests check that they are up to date. Never edit `zz_generated.*.go` by hand.

The files in `examples/` are loaded by the tests, so they must always stay valid.

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
