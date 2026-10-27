# Copyright 2026 The Packtrix Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Build information embedded in the binary, see internal/version. The version
# is the tag of the current commit, or the commit itself.
GIT_VERSION := $(shell git describe --tags --exact-match 2>/dev/null || git rev-parse HEAD 2>/dev/null)
GIT_COMMIT := $(shell git rev-parse HEAD 2>/dev/null)
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

VERSION_PKG := github.com/packtrix/packtrix/internal/version
LDFLAGS := -w \
	-X $(VERSION_PKG).version=$(GIT_VERSION) \
	-X $(VERSION_PKG).commit=$(GIT_COMMIT) \
	-X $(VERSION_PKG).date=$(DATE)

# Binaries are built for this machine unless GOOS and GOARCH are given, e.g.
# make build GOOS=linux GOARCH=arm64.
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
GOBUILDFLAGS ?= -v -trimpath

LOCALBIN ?= bin
CMDS := $(notdir $(wildcard cmd/*))

GOLANGCI_LINT_VERSION ?= v2.14.0
GOLANGCI_LINT := $(LOCALBIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)

HUGO_VERSION ?= v0.167.0
HUGO := $(LOCALBIN)/hugo-$(HUGO_VERSION)

# The release workflow pins the same versions.
GORELEASER_VERSION ?= v2.18.3
GORELEASER := $(LOCALBIN)/goreleaser-$(GORELEASER_VERSION)
SYFT_VERSION ?= v1.54.0
SYFT := $(LOCALBIN)/syft-$(SYFT_VERSION)

GOVULNCHECK_VERSION ?= v1.9.0
GOVULNCHECK := $(LOCALBIN)/govulncheck-$(GOVULNCHECK_VERSION)

.PHONY: build
build: $(addprefix $(LOCALBIN)/,$(CMDS))

# Builds the static binary of cmd/<name>. It always runs, and Go's build cache
# decides what needs compiling.
$(LOCALBIN)/%: FORCE
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
	go build $(GOBUILDFLAGS) \
	-ldflags '$(LDFLAGS)' \
	-o $@ ./cmd/$*

.PHONY: clean
clean:
	rm -rf $(LOCALBIN)

# Skips the tests that download from live upstream repositories.
.PHONY: test
test:
	go test -race -short -coverprofile=coverage.out ./...

# Runs only the tests that download from live upstream repositories.
.PHONY: test-live
test-live:
	go test -race -run '^TestLive' ./...

# Publishes repositories from live upstream repositories and installs from
# them in clean containers of each distro, with docker, and compares the
# dependency closures with apt's (TestCompare). E2E_RUN selects tests, such
# as E2E_RUN=TestInstall/debian-13.
.PHONY: test-e2e
test-e2e:
	go test -tags e2e -count=1 -timeout 30m -v -run '$(E2E_RUN)' ./test/e2e/...

# Fails if the code calls a function with a known vulnerability, in a module
# or in the standard library. It reads the Go vulnerability database online.
.PHONY: vulncheck
vulncheck: $(GOVULNCHECK)
	$(GOVULNCHECK) ./...

$(GOVULNCHECK):
	GOBIN=$(abspath $(LOCALBIN)) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	mv $(LOCALBIN)/govulncheck $@

# Fails, showing the difference, if go.mod or go.sum aren't tidy.
.PHONY: verify-gomod
verify-gomod:
	go mod tidy -diff

# Runs every check that pull requests run.
.PHONY: verify
verify: verify-boilerplate verify-codegen verify-schema verify-gomod lint test

.PHONY: lint
lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...

# Installs the pinned golangci-lint. The version is part of the file name, so
# changing GOLANGCI_LINT_VERSION installs the new version.
$(GOLANGCI_LINT):
	GOBIN=$(abspath $(LOCALBIN)) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv $(LOCALBIN)/golangci-lint $@

# Builds the website into site/public. SITE_BASE_URL overrides the base URL.
.PHONY: site
site: $(HUGO)
	$(HUGO) --source site --minify $(if $(SITE_BASE_URL),--baseURL $(SITE_BASE_URL))

# Serves the website at http://localhost:1313/ and rebuilds it on every change.
.PHONY: site-serve
site-serve: $(HUGO)
	$(HUGO) server --source site

# Installs the pinned Hugo, the same way as golangci-lint. Hugo may need a newer
# Go than Packtrix; GOTOOLCHAIN=auto lets go install fetch it, verified against
# the Go checksum database.
$(HUGO):
	GOTOOLCHAIN=auto GOBIN=$(abspath $(LOCALBIN)) go install github.com/gohugoio/hugo@$(HUGO_VERSION)
	mv $(LOCALBIN)/hugo $@

# Builds a release into dist/ as the release workflow does, without signing
# or publishing it: the archives, checksums.txt and the SBOMs.
.PHONY: release-snapshot
release-snapshot: $(GORELEASER) $(SYFT)
	ln -sf $(notdir $(SYFT)) $(LOCALBIN)/syft
	PATH="$(abspath $(LOCALBIN)):$$PATH" $(GORELEASER) release --snapshot --clean --skip=sign

$(GORELEASER):
	GOTOOLCHAIN=auto GOBIN=$(abspath $(LOCALBIN)) go install github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION)
	mv $(LOCALBIN)/goreleaser $@

$(SYFT):
	GOTOOLCHAIN=auto GOBIN=$(abspath $(LOCALBIN)) go install github.com/anchore/syft/cmd/syft@$(SYFT_VERSION)
	mv $(LOCALBIN)/syft $@

.PHONY: update-codegen
update-codegen:
	./hack/update-codegen.sh

.PHONY: verify-codegen
verify-codegen:
	./hack/verify-codegen.sh

# Regenerates the JSON Schema of the manifest from the v1alpha1 types.
.PHONY: update-schema
update-schema:
	go run ./hack/schema -o schemas/v1alpha1/offlinerepository.json

# Fails, showing the difference, if the JSON Schema of the manifest is out of
# date.
.PHONY: verify-schema
verify-schema:
	@go run ./hack/schema | diff -u schemas/v1alpha1/offlinerepository.json - \
		|| { echo "The schema is out of date: run make update-schema and commit the result." >&2; exit 1; }

.PHONY: verify-boilerplate
verify-boilerplate:
	./hack/verify-boilerplate.sh

.PHONY: FORCE
FORCE:
