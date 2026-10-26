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

// Package publish carries out a lock: it downloads the locked packages,
// builds the repositories and the client files into a new snapshot, and
// switches the published directory to it in one step. Machines never see
// a half-built repository, and a sync that fails leaves the published one
// as it was.
//
// The publish path holds:
//
//	current -> snapshots/<time>   the published directory, which is served
//	snapshots/<time>/             apt/, keys/, clients/, packtrix.lock
//	pool/                         the package pool, hard-linked into snapshots
package publish

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/packtrix/packtrix/internal/build"
	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/internal/download"
	"github.com/packtrix/packtrix/internal/expand"
	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/pgp"
	"github.com/packtrix/packtrix/internal/pool"
	repoapt "github.com/packtrix/packtrix/internal/repo/apt"
	"github.com/packtrix/packtrix/pkg/apis/packtrix"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/manifest"
)

// Options configure a sync.
type Options struct {
	Manifest *packtrix.OfflineRepository
	// ManifestDir is the manifest's directory, which relative paths start
	// from.
	ManifestDir string
	// Lock is the lock to carry out, planned from Manifest.
	Lock    *packtrix.Lock
	Catalog *catalog.Catalog
	Client  *fetch.Client
	// Cache holds the upstream metadata that plan read, for the
	// repositories' metadata.
	Cache *pool.Pool
	// Signer signs the repositories.
	Signer *pgp.Signer
	// BaseURL is where the published directory is served, for the client
	// files.
	BaseURL string
	// Downloaded, if set, is told when each target's packages are in the
	// pool.
	Downloaded func(download.Target)
}

// Result is what a sync published.
type Result struct {
	// Dir is the published directory: <publish path>/current.
	Dir string
	// Snapshot is the name of the snapshot that Dir points to.
	Snapshot string
	Targets  []build.Target
}

// Path returns where a manifest publishes. A relative publish.path starts
// from the manifest's directory.
func Path(m *packtrix.OfflineRepository, manifestDir string) string {
	if filepath.IsAbs(m.Spec.Publish.Path) {
		return m.Spec.Publish.Path
	}
	return filepath.Join(manifestDir, m.Spec.Publish.Path)
}

// Sync carries out a lock. The lock must have been planned from the
// manifest; manifest.CheckLock tells.
func Sync(ctx context.Context, opts Options) (*Result, error) {
	root := Path(opts.Manifest, opts.ManifestDir)
	snapshots := filepath.Join(root, "snapshots")
	if err := os.MkdirAll(snapshots, 0o755); err != nil {
		return nil, err
	}
	unlock, err := lock(root)
	if err != nil {
		return nil, err
	}
	defer unlock()

	packages, err := pool.Open(filepath.Join(root, "pool"))
	if err != nil {
		return nil, err
	}
	expanded, err := expand.Expand(opts.Manifest, opts.Catalog, opts.ManifestDir)
	if err != nil {
		return nil, err
	}
	auth := func(target, repository string) (*fetch.Auth, error) {
		for _, t := range expanded.Targets {
			for _, a := range t.Architectures {
				for _, r := range a.Repositories {
					if t.Name == target && r.Name == repository {
						return fetch.ResolveAuth(r.Auth, opts.ManifestDir)
					}
				}
			}
		}
		return nil, nil
	}
	if _, err := download.Packages(ctx, opts.Client, packages, opts.Lock, download.Options{Auth: auth, Done: opts.Downloaded}); err != nil {
		return nil, err
	}

	// Build the snapshot where the published directory can't see it.
	staging, err := os.MkdirTemp(snapshots, ".new-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	targets, err := build.APT(ctx, opts.Lock, expanded, staging, build.Options{
		Reader:      &repoapt.Reader{Client: opts.Client, Cache: opts.Cache},
		Packages:    packages,
		Signer:      opts.Signer,
		ManifestDir: opts.ManifestDir,
	})
	if err != nil {
		return nil, err
	}
	if err := build.Clients(staging, targets, opts.Signer, opts.BaseURL); err != nil {
		return nil, err
	}
	lockData, err := manifest.MarshalLock(opts.Lock)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(staging, manifest.LockName), lockData, 0o644); err != nil {
		return nil, err
	}
	// MkdirTemp makes the directory private; web servers must read it.
	if err := os.Chmod(staging, 0o755); err != nil {
		return nil, err
	}

	name := snapshotName(snapshots, time.Now())
	if err := os.Rename(staging, filepath.Join(snapshots, name)); err != nil {
		return nil, err
	}
	if err := switchCurrent(root, name); err != nil {
		return nil, err
	}
	if err := prune(snapshots, name, *opts.Manifest.Spec.Publish.KeepSnapshots); err != nil {
		return nil, fmt.Errorf("removing old snapshots: %w", err)
	}

	current := filepath.Join(root, "current")
	for i := range targets {
		targets[i].Dir = filepath.Join(current, "apt", targets[i].Name)
	}
	return &Result{Dir: current, Snapshot: name, Targets: targets}, nil
}

// lock takes the publish path's lock, so two syncs can't write it at once.
func lock(root string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(root, ".lock"), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("another sync is writing %s", root)
		}
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}

// snapshotName names a snapshot after the time it was made, such as
// "2026-10-26T101500Z", so names sort by age.
func snapshotName(snapshots string, t time.Time) string {
	name := t.UTC().Format("2006-01-02T150405Z")
	for i := 2; ; i++ {
		if _, err := os.Lstat(filepath.Join(snapshots, name)); os.IsNotExist(err) {
			return name
		}
		name = t.UTC().Format("2006-01-02T150405Z") + fmt.Sprintf("-%d", i)
	}
}

// switchCurrent points current at a snapshot. Renaming a new link over the
// old one replaces it in one step.
func switchCurrent(root, name string) error {
	next := filepath.Join(root, ".current")
	if err := os.Remove(next); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Symlink(filepath.Join("snapshots", name), next); err != nil {
		return err
	}
	current := filepath.Join(root, "current")
	if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s isn't a link to a snapshot; move it aside", current)
	}
	return os.Rename(next, current)
}

// prune removes the snapshots beyond the current one and the keep before it,
// and what interrupted syncs left.
func prune(snapshots, current string, keep int) error {
	entries, err := os.ReadDir(snapshots)
	if err != nil {
		return err
	}
	var older []string
	for _, e := range entries {
		switch name := e.Name(); {
		case strings.HasPrefix(name, ".new-"):
			if err := os.RemoveAll(filepath.Join(snapshots, name)); err != nil {
				return err
			}
		case name != current:
			older = append(older, name)
		}
	}
	slices.Sort(older)
	for _, name := range older[:max(0, len(older)-keep)] {
		if err := os.RemoveAll(filepath.Join(snapshots, name)); err != nil {
			return err
		}
	}
	return nil
}
