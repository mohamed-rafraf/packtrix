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

package packtrix

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Lock records exactly what plan resolved for a manifest, and what sync
// downloads: every package of every target and architecture, and where it
// comes from.
type Lock struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec LockSpec
}

// LockSpec is the content of a lock.
type LockSpec struct {
	// Manifest is the digest of the manifest the lock was planned from, as
	// "sha256:<hex>".
	Manifest string
	// Catalog is the version of the built-in catalog used.
	Catalog string
	Targets []LockTarget
}

// LockTarget is what one target needs for one architecture.
type LockTarget struct {
	Name         string
	Architecture string
	Format       RepoType
	// Distribution is what the repositories' metadata says, such as
	// "Ubuntu noble".
	Distribution string
	Repositories []LockRepository
	Packages     []LockPackage
}

// LockRepository is an upstream repository, as plan verified it.
type LockRepository struct {
	Name       string
	URL        string
	Suite      string
	Components []string
	// Release is the digest of the signed metadata that was verified, as
	// "sha256:<hex>".
	Release string
	// Signer is the fingerprint of the key that signed the metadata.
	Signer string
}

// LockPackage is a package to download.
type LockPackage struct {
	Name string
	// Epoch is the epoch of an RPM package. APT versions include theirs.
	Epoch        int
	Version      string
	Architecture string
	Repository   string
	URL          string
	SHA256       string
	Size         int64
	// SignedBy is the fingerprint of the key that signed an RPM package.
	SignedBy string
	// Why is the chain of packages that need it, from the nearest to the
	// listed one. It is empty for a listed package.
	Why []string
}
