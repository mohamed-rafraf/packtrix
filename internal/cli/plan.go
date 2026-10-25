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

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/internal/expand"
	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/plan"
	"github.com/packtrix/packtrix/internal/pool"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
)

func newPlanCommand() *cobra.Command {
	var file, output, caFile, lockFile, cacheDir string
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Resolve every target and show what will be downloaded",
		Long: `Plan reads and verifies the metadata of every upstream repository, resolves
the dependencies of each target and architecture, and prints the packages to
download, with their sizes and why each one is needed. It downloads no
package.

It writes the result to packtrix.lock, next to the manifest, which sync
carries out. Commit it: an update is a diff of the lock.

It plans every target it can, reports every failure, and fails if any target
failed. The lock is only written when every target is planned.`,
		Example: `  packtrix plan
  packtrix plan -f examples/k8s-node-prereqs.yaml -o json
  packtrix plan --ca-file /etc/ssl/certs/corporate-proxy.pem`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if output != "" && output != "json" {
				return fmt.Errorf("unsupported output format %q (supported: json)", output)
			}
			m, err := manifest.LoadFile(file)
			if err != nil {
				return err
			}
			c, err := catalog.Builtin()
			if err != nil {
				return err
			}
			client, err := fetch.New(fetch.Options{CAFile: caFile})
			if err != nil {
				return err
			}
			cache, err := openCache(cacheDir)
			if err != nil {
				return err
			}
			stderr := cmd.ErrOrStderr()
			p, err := plan.Run(cmd.Context(), m, c, client, plan.Options{
				Dir:   filepath.Dir(file),
				Cache: cache,
				Reading: func(target, arch string, r expand.Repository) {
					fmt.Fprintf(stderr, "Reading %s (%s): %s, %s %s\n", target, arch, r.Name, r.URL, r.Suite)
				},
			})
			if p != nil {
				var werr error
				if output == "json" {
					werr = writeJSON(cmd.OutOrStdout(), p)
				} else {
					werr = writePlan(cmd.OutOrStdout(), p)
				}
				if err == nil {
					err = werr
				}
			}
			if cache != nil {
				// Indexes that no run used for a while belong to old versions
				// of the repositories.
				_, _ = cache.Prune(time.Now().Add(-cacheMaxAge))
			}
			if lockFile == "" {
				lockFile = filepath.Join(filepath.Dir(file), manifest.LockName)
			}
			if err != nil {
				fmt.Fprintf(stderr, "%s wasn't written: it is only written when every target is planned\n", lockFile)
				return err
			}
			digest, err := manifest.Digest(m)
			if err != nil {
				return err
			}
			changed, err := manifest.WriteLockFile(lockFile, p.Lock(m.Name, digest, c.Version))
			if err != nil {
				return err
			}
			if changed {
				fmt.Fprintf(stderr, "Wrote %s\n", lockFile)
			} else {
				fmt.Fprintf(stderr, "%s is unchanged\n", lockFile)
			}
			return nil
		},
	}
	addManifestFlag(cmd, &file)
	cmd.Flags().StringVarP(&output, "output", "o", "", "print the plan as json")
	cmd.Flags().StringVar(&caFile, "ca-file", "", "PEM file of certificate authorities to trust, such as a TLS-inspecting proxy's")
	cmd.Flags().StringVar(&lockFile, "lock", "", "path of the lock to write (default packtrix.lock next to the manifest)")
	addCacheFlag(cmd, &cacheDir)
	return cmd
}

// cacheMaxAge is how long the cache keeps an index that no run uses.
const cacheMaxAge = 30 * 24 * time.Hour

// addCacheFlag registers the --cache-dir flag on commands that read
// upstream repositories.
func addCacheFlag(cmd *cobra.Command, dir *string) {
	cmd.Flags().StringVar(dir, "cache-dir", "", "directory of the cache of upstream indexes (default $XDG_CACHE_HOME/packtrix, usually ~/.cache/packtrix)")
}

// openCache opens the cache of upstream indexes in dir, or in the user's
// cache directory. Without one, it returns nil, and indexes aren't cached.
func openCache(dir string) (*pool.Pool, error) {
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil, nil
		}
		dir = filepath.Join(base, "packtrix")
	}
	return pool.Open(filepath.Join(dir, "indexes"))
}

func writeJSON(out io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = out.Write(append(data, '\n'))
	return err
}

// writePlan prints, for each target and architecture, its repositories and
// the packages to download, then the totals. Packages that several targets
// share count once.
func writePlan(out io.Writer, p *plan.Plan) error {
	files := map[string]int64{}
	for i, t := range p.Targets {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "%s (%s): %s, %d packages, %s\n", t.Name, t.Architecture, t.Distribution, len(t.Packages), size(t.Size()))

		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  REPOSITORY\tSUITE\tPACKAGES\tSIGNED BY")
		for _, r := range t.Repositories {
			name := r.Name
			if r.Extra {
				name += " (extra)"
			}
			fmt.Fprintf(w, "  %s\t%s\t%d\t%s\n", name, r.Suite, r.Packages, r.Signer)
		}
		if err := w.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(out)
		w = tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  PACKAGE\tVERSION\tSIZE\tWHY")
		for _, pkg := range t.Packages {
			why := "listed"
			if len(pkg.Why) > 0 {
				chain := slices.Clone(pkg.Why)
				slices.Reverse(chain)
				why = strings.Join(chain, " → ")
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", pkg.Name, pkg.Version, size(pkg.Size), why)
			files[pkg.SHA256] = pkg.Size
		}
		if err := w.Flush(); err != nil {
			return err
		}
		for _, x := range t.Excluded {
			fmt.Fprintf(out, "  Excluded: %s, needed by %s\n", x.Name, strings.Join(x.NeededBy, ", "))
		}
	}
	var total int64
	for _, n := range files {
		total += n
	}
	_, err := fmt.Fprintf(out, "\n%d target(s), %d package files to download, %s\n", len(p.Targets), len(files), size(total))
	return err
}

// size formats a number of bytes, such as "1.4 MiB".
func size(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	value, unit := float64(n)/1024, 0
	for value >= 1024 && unit < 4 {
		value /= 1024
		unit++
	}
	return fmt.Sprintf("%.1f %ciB", value, "KMGTP"[unit])
}
