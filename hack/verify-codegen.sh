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

# Checks that the generated code under pkg/apis is up to date by regenerating
# it. Out-of-date files are left regenerated.

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# Prints a checksum line per generated file.
checksums() {
  find pkg/apis -name 'zz_generated.*.go' -exec cksum {} + | sort -k3
}

before=$(checksums)
./hack/update-codegen.sh >/dev/null
after=$(checksums)

if [[ $before != "$after" ]]; then
  echo "Generated code is out of date:" >&2
  diff <(echo "$before") <(echo "$after") | awk '/^[<>]/ { print "  " $NF }' | sort -u >&2
  echo >&2
  echo "Run make update-codegen and commit the result." >&2
  exit 1
fi
