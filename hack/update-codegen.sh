#!/usr/bin/env bash

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

# Regenerates the deepcopy, defaulter and conversion code of the API packages
# under pkg/apis. Packages opt in with +k8s:*-gen tags in their doc.go.

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# The code-generator version is pinned by the tool directives in go.mod.
go mod download k8s.io/code-generator
CODEGEN_PKG=$(go list -m -f '{{.Dir}}' k8s.io/code-generator)
source "${CODEGEN_PKG}/kube_codegen.sh"

kube::codegen::gen_helpers \
  --boilerplate hack/boilerplate/boilerplate.generatego.txt \
  pkg/apis
