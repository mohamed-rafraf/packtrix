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
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/packtrix/packtrix/internal/build"
	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/internal/download"
	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/pgp"
	"github.com/packtrix/packtrix/internal/publish"
	"github.com/packtrix/packtrix/internal/secret"
	"github.com/packtrix/packtrix/pkg/apis/packtrix"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
)

func newSyncCommand() *cobra.Command {
	var file, lockFile, caFile, cacheDir, baseURL string
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Download the locked packages and publish the repositories",
		Long: `Sync carries out packtrix.lock: it downloads the locked packages into the
package pool, checking each against the lock, builds one repository per target,
signed with publish.signing.key, and writes the client files of the machines.

The result goes to publish.path, as a new snapshot that current points to.
current switches to it in one step, so machines never see a half-built
repository, and a sync that fails leaves the published one as it was.

Run packtrix plan first: sync refuses a lock planned from another version of
the manifest.`,
		Example: `  packtrix plan && packtrix sync --base-url http://repo.internal
  PACKTRIX_SIGNING_KEY="$(cat key.asc)" packtrix sync -f packtrix.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := manifest.LoadFile(file)
			if err != nil {
				return err
			}
			dir := filepath.Dir(file)
			if lockFile == "" {
				lockFile = filepath.Join(dir, manifest.LockName)
			}
			l, err := manifest.LoadLockFile(lockFile)
			if err != nil {
				return err
			}
			if err := manifest.CheckLock(l, m); err != nil {
				return fmt.Errorf("%s: %w", lockFile, err)
			}
			signer, err := loadSigner(m, dir)
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
			res, err := publish.Sync(cmd.Context(), publish.Options{
				Manifest: m, ManifestDir: dir, Lock: l, Catalog: c, Client: client, Cache: cache, Signer: signer, BaseURL: baseURL,
				Downloaded: func(t download.Target) {
					fmt.Fprintf(stderr, "Downloaded %s (%s): %d packages, %d new, %s\n", t.Name, t.Architecture, t.Packages, t.Downloaded, size(t.Bytes))
				},
			})
			if err != nil {
				return err
			}
			return writeSync(cmd.OutOrStdout(), res, baseURL)
		},
	}
	addManifestFlag(cmd, &file)
	cmd.Flags().StringVar(&lockFile, "lock", "", "path of the lock to carry out (default packtrix.lock next to the manifest)")
	cmd.Flags().StringVar(&caFile, "ca-file", "", "PEM file of certificate authorities to trust, such as a TLS-inspecting proxy's")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "URL where the published directory is served, for the client files, such as http://repo.internal")
	addCacheFlag(cmd, &cacheDir)
	return cmd
}

// loadSigner loads the key that publish.signing.key references.
func loadSigner(m *packtrix.OfflineRepository, dir string) (*pgp.Signer, error) {
	ref := m.Spec.Publish.Signing.Key
	if ref == "" {
		return nil, errors.New("publish.signing.key is required to sign the repositories: env:NAME or file:/path, holding an OpenPGP private key")
	}
	key, err := secret.Resolve(ref, dir)
	if err != nil {
		return nil, fmt.Errorf("publish.signing.key: %w", err)
	}
	signer, err := pgp.LoadSigner(key.Bytes())
	if err != nil {
		return nil, fmt.Errorf("publish.signing.key: %w", err)
	}
	return signer, nil
}

// writeSync prints what a sync published, and how machines use it.
func writeSync(out io.Writer, res *publish.Result, baseURL string) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TARGET\tSUITE\tARCHITECTURES\tPACKAGES\tSIZE")
	for _, t := range res.Targets {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", t.Name, t.Codename, strings.Join(t.Architectures, ", "), t.Files, size(t.Bytes))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nPublished %s (snapshot %s).\n", res.Dir, res.Snapshot)
	fmt.Fprintf(out, "Each machine needs two files: see %s.\n", filepath.Join(res.Dir, "clients", "README"))
	if baseURL == "" {
		fmt.Fprintf(out, "The client files use %s: set --base-url to where %s is served.\n", build.Placeholder, res.Dir)
	}
	return nil
}
