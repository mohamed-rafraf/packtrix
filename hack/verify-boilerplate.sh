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

# Checks that every Go, shell, YAML and Makefile file starts with the license
# header from hack/boilerplate/. Generated files and anything under vendor/ or
# testdata/ are skipped.

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# Prints the first $3 lines of file $1 where the header should be, skipping what
# may come before it: a shebang in shell scripts, build constraints in Go. The
# copyright year is replaced by YEAR, as in the templates.
header_of() {
  awk -v kind="$2" -v lines="$3" '
    !started && kind == "sh" && FNR == 1 && /^#!/ { skipped = 1; next }
    !started && kind == "go" && /^\/\/(go:build| \+build)/ { skipped = 1; next }
    !started && skipped && /^$/ { next }
    { started = 1 }
    !year && /Copyright / {
      year = 1
      # A literal YEAR means the template was copied without filling it in.
      sub(/Copyright YEAR /, "Copyright <missing year> ")
      sub(/Copyright 20[0-9][0-9] /, "Copyright YEAR ")
    }
    { print }
    ++printed == lines { exit }
  ' "$1"
}

status=0

# Untracked files are included so that new files are caught before a commit.
while IFS= read -r -d '' file; do
  case "$file" in
    vendor/* | testdata/* | */testdata/*) continue ;;
    *.go) kind=go ;;
    *.sh) kind=sh ;;
    *.yaml | *.yml) kind=yaml ;;
    Makefile | */Makefile | *.mk) kind=Makefile ;;
    *) continue ;;
  esac

  # Deleted but not yet committed.
  [[ -f $file ]] || continue

  if grep -qE '^(//|#) Code generated .* DO NOT EDIT\.$' "$file"; then
    continue
  fi

  template=hack/boilerplate/boilerplate.$kind.txt
  if [[ "$(header_of "$file" "$kind" "$(wc -l <"$template")")" != "$(<"$template")" ]]; then
    echo "$file: missing or incorrect license header" >&2
    status=1
  fi
done < <(git ls-files -z --cached --others --exclude-standard)

if ((status)); then
  echo >&2
  echo "Start each file with the header from hack/boilerplate/boilerplate.<type>.txt," >&2
  echo "with YEAR replaced by the current year." >&2
fi

exit "$status"
