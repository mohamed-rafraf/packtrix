/*
Copyright 2026 The Packtrix Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package apt

import (
	"fmt"
	"strings"

	"github.com/packtrix/packtrix/internal/deb"
	"github.com/packtrix/packtrix/internal/resolve"
	"github.com/packtrix/packtrix/pkg/apis/packtrix"
)

// Baseline returns the packages that the machines of a target already have,
// for a resolve.baseline setting. release is as for MinimalBaseline.
func Baseline(setting, arch string, release []*deb.Package) ([]*deb.Package, error) {
	switch {
	case setting == packtrix.BaselineMinimal:
		return MinimalBaseline(arch, release)
	case setting == packtrix.BaselineNone:
		return nil, nil
	case strings.HasPrefix(setting, packtrix.BaselineLockPrefix):
		return nil, fmt.Errorf("baseline %q: baselines from a lockfile aren't supported yet; they come with host discovery", setting)
	default:
		return nil, fmt.Errorf("unknown baseline %q", setting)
	}
}

// MinimalBaseline returns what a minimal installation of a distro release
// has: the packages that are Essential or have Priority required or
// important, which the installer selects, and their dependencies.
//
// release holds the packages of the release suite's main component, such as
// Ubuntu 24.04's noble, without noble-updates: machines installed from the
// release media have the release versions, so a package that needs a newer
// version gets it from the repository.
func MinimalBaseline(arch string, release []*deb.Package) ([]*deb.Package, error) {
	ix := NewIndex(arch, release)
	var seeds []*deb.Package
	for _, p := range release {
		versions := ix.packages[p.Name]
		if len(versions) > 0 && versions[0] == p && (p.Essential || p.Priority == "required" || p.Priority == "important") {
			seeds = append(seeds, p)
		}
	}
	result, err := resolve.Closure(NewModel(ix, nil, Options{}), seeds)
	if err != nil {
		return nil, fmt.Errorf("minimal baseline: %w", err)
	}
	return result.Packages, nil
}
