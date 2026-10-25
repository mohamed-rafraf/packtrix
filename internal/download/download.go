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

// Package download fetches the packages that a lock lists into the package
// pool, and checks each one against the size and SHA-256 that the lock
// took from the verified upstream index.
package download

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/packtrix/packtrix/internal/fetch"
	"github.com/packtrix/packtrix/internal/pool"
	"github.com/packtrix/packtrix/pkg/apis/packtrix"
)

// Options configure a run.
type Options struct {
	// Auth returns the credentials of a target's repository, or nil.
	Auth func(target, repository string) (*fetch.Auth, error)
	// Workers is the number of files downloaded at once. Zero means 8.
	Workers int
	// Done, if set, is told when each target's packages are all in the
	// pool.
	Done func(Target)
}

// Target sums up what a target and architecture needed.
type Target struct {
	Name         string
	Architecture string
	// Packages is the number of packages the lock lists.
	Packages int
	// Downloaded and Bytes count the files this run downloaded.
	Downloaded int
	Bytes      int64
}

// Packages downloads the packages of every target of a lock that the pool
// doesn't hold yet. A file that several targets use is downloaded once. It
// stops at the first file that fails or doesn't match the lock, and returns
// a summary per target.
func Packages(ctx context.Context, c *fetch.Client, p *pool.Pool, l *packtrix.Lock, opts Options) ([]Target, error) {
	workers := opts.Workers
	if workers == 0 {
		workers = 8
	}
	var out []Target
	for _, t := range l.Spec.Targets {
		sum := Target{Name: t.Name, Architecture: t.Architecture, Packages: len(t.Packages)}
		var missing []packtrix.LockPackage
		seen := map[string]bool{}
		for _, pkg := range t.Packages {
			if !p.Has(pkg.SHA256) && !seen[pkg.SHA256] {
				seen[pkg.SHA256] = true
				missing = append(missing, pkg)
			}
		}
		if err := fetchAll(ctx, c, p, t, missing, workers, opts.Auth); err != nil {
			return out, fmt.Errorf("target %s (%s): %w", t.Name, t.Architecture, err)
		}
		sum.Downloaded = len(missing)
		for _, pkg := range missing {
			sum.Bytes += pkg.Size
		}
		out = append(out, sum)
		if opts.Done != nil {
			opts.Done(sum)
		}
	}
	return out, nil
}

// fetchAll downloads packages with a few workers, and cancels the others at
// the first failure.
func fetchAll(ctx context.Context, c *fetch.Client, p *pool.Pool, t packtrix.LockTarget, pkgs []packtrix.LockPackage, workers int, auth func(target, repository string) (*fetch.Auth, error)) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	jobs := make(chan packtrix.LockPackage)
	var wg sync.WaitGroup
	for range min(workers, len(pkgs)) {
		wg.Go(func() {
			for pkg := range jobs {
				if err := fetchOne(ctx, c, p, t.Name, pkg, auth); err != nil {
					cancel(err)
				}
			}
		})
	}
	for _, pkg := range pkgs {
		select {
		case jobs <- pkg:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()
	return context.Cause(ctx)
}

// fetchOne downloads a package into the pool. A download that an earlier
// run finished but didn't commit is committed without fetching it again.
func fetchOne(ctx context.Context, c *fetch.Client, p *pool.Pool, target string, pkg packtrix.LockPackage, auth func(target, repository string) (*fetch.Auth, error)) error {
	if ctx.Err() != nil {
		return nil
	}
	describe := func(err error) error {
		return fmt.Errorf("package %s %s (%s): %w", pkg.Name, pkg.Version, pkg.URL, err)
	}
	if info, err := os.Stat(p.Partial(pkg.SHA256)); err == nil && info.Size() == pkg.Size {
		if p.Commit(pkg.SHA256, pkg.Size) == nil {
			return nil
		}
	}
	var a *fetch.Auth
	if auth != nil {
		var err error
		if a, err = auth(target, pkg.Repository); err != nil {
			return describe(err)
		}
	}
	if err := c.Download(ctx, pkg.URL, a, p.Partial(pkg.SHA256)); err != nil {
		return describe(err)
	}
	if err := p.Commit(pkg.SHA256, pkg.Size); err != nil {
		return describe(fmt.Errorf("doesn't match the lock: %w", err))
	}
	return nil
}
