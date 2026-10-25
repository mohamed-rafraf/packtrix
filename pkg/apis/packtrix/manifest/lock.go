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

package manifest

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/packtrix/packtrix/pkg/apis/packtrix"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/scheme"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/validation"
)

// LockName is the name of the lock's file, next to the manifest.
const LockName = "packtrix.lock"

var lockKind = packtrix.Kind("Lock")

// Digest returns the digest of a manifest, as "sha256:<hex>". It is taken
// from the manifest with its defaults, in the preferred API version, so
// comments and formatting don't change it.
func Digest(r *packtrix.OfflineRepository) (string, error) {
	data, err := Marshal(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// CheckLock returns an error when a lock wasn't planned from this version
// of the manifest.
func CheckLock(l *packtrix.Lock, r *packtrix.OfflineRepository) error {
	digest, err := Digest(r)
	if err != nil {
		return err
	}
	if l.Spec.Manifest != digest {
		return errors.New("the lock was planned from another version of the manifest; run packtrix plan")
	}
	return nil
}

// ParseLock decodes a YAML lock and converts it to the internal API. It
// rejects unknown and duplicate fields, but doesn't validate; LoadLockFile
// does.
func ParseLock(data []byte) (*packtrix.Lock, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("the lock is empty")
	}
	var meta metav1.TypeMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, decodeError(err)
	}
	versioned, err := newVersioned(meta, lockKind, "lock")
	if err != nil {
		return nil, err
	}
	if err := yaml.UnmarshalStrict(data, versioned); err != nil {
		return nil, decodeError(err)
	}
	scheme.Scheme.Default(versioned)
	l := &packtrix.Lock{}
	if err := scheme.Scheme.Convert(versioned, l, nil); err != nil {
		return nil, err
	}
	return l, nil
}

// LoadLockFile reads and validates the lock at path.
func LoadLockFile(path string) (*packtrix.Lock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	l, err := ParseLock(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := validation.ValidateLock(l); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return l, nil
}

// MarshalLock writes a lock as YAML in the preferred API version, with its
// lists sorted, so the same lock always gives the same bytes.
func MarshalLock(l *packtrix.Lock) ([]byte, error) {
	sorted := l.DeepCopy()
	sortLock(sorted)
	versioned, err := scheme.Scheme.ConvertToVersion(sorted, preferredVersion())
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(versioned)
}

// WriteLockFile writes a lock to path, replacing the file in one step, and
// reports whether its content changed.
func WriteLockFile(path string, l *packtrix.Lock) (changed bool, err error) {
	data, err := MarshalLock(l)
	if err != nil {
		return false, err
	}
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return false, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return false, err
	}
	return true, os.Rename(tmp.Name(), path)
}

func sortLock(l *packtrix.Lock) {
	slices.SortFunc(l.Spec.Targets, func(a, b packtrix.LockTarget) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.Architecture, b.Architecture))
	})
	for i := range l.Spec.Targets {
		t := &l.Spec.Targets[i]
		slices.SortFunc(t.Repositories, func(a, b packtrix.LockRepository) int {
			return strings.Compare(a.Name, b.Name)
		})
		slices.SortFunc(t.Packages, func(a, b packtrix.LockPackage) int {
			return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.Version, b.Version), strings.Compare(a.Architecture, b.Architecture))
		})
	}
}
