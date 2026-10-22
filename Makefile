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

.PHONY: lint
lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...

# Installs the pinned golangci-lint. The version is part of the file name, so
# changing GOLANGCI_LINT_VERSION installs the new version.
$(GOLANGCI_LINT):
	GOBIN=$(abspath $(LOCALBIN)) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv $(LOCALBIN)/golangci-lint $@

.PHONY: update-codegen
update-codegen:
	./hack/update-codegen.sh

.PHONY: verify-codegen
verify-codegen:
	./hack/verify-codegen.sh

.PHONY: verify-boilerplate
verify-boilerplate:
	./hack/verify-boilerplate.sh

.PHONY: FORCE
FORCE:
