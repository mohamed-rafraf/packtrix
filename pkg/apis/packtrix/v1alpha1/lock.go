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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Lock records exactly what plan resolved for a manifest, and what sync
// downloads: every package of every target and architecture, and where it
// comes from. It is written to packtrix.lock, next to the manifest. Its
// lists are sorted and it holds no timestamp, so planning the same thing
// twice writes the same bytes, and an update is a readable diff.
type Lock struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec LockSpec `json:"spec"`
}

// LockSpec is the content of a lock.
type LockSpec struct {
	// Manifest is the digest of the manifest the lock was planned from, as
	// "sha256:<hex>", taken after defaults so that comments and formatting
	// don't count. A lock whose digest differs from the manifest's is stale.
	Manifest string `json:"manifest"`
	// Catalog is the version of the built-in catalog used.
	Catalog string `json:"catalog"`
	// Targets are sorted by name, then architecture.
	Targets []LockTarget `json:"targets"`
}

// LockTarget is what one target needs for one architecture.
type LockTarget struct {
	Name         string   `json:"name"`
	Architecture string   `json:"architecture"`
	Format       RepoType `json:"format"`
	// Distribution is what the repositories' metadata says, such as
	// "Ubuntu noble".
	Distribution string `json:"distribution,omitempty"`
	// Repositories are sorted by name.
	Repositories []LockRepository `json:"repositories"`
	// Packages are sorted by name, then version.
	Packages []LockPackage `json:"packages"`
}

// LockRepository is an upstream repository, as plan verified it.
type LockRepository struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Suite and Components are those of an APT repository.
	Suite      string   `json:"suite,omitempty"`
	Components []string `json:"components,omitempty"`
	// Release is the digest of the signed metadata that was verified, the
	// Release file for APT, as "sha256:<hex>". It lists the digest of every
	// index, so it changes whenever the repository does.
	Release string `json:"release"`
	// Signer is the fingerprint of the key that signed the metadata.
	Signer string `json:"signer"`
}

// LockPackage is a package to download.
type LockPackage struct {
	Name string `json:"name"`
	// Epoch is the epoch of an RPM package. APT versions include theirs.
	Epoch int `json:"epoch,omitempty"`
	// Version is the full version: "1:2.39-0ubuntu8" for APT,
	// "8.5.0-1.el9" (version-release) for RPM.
	Version      string `json:"version"`
	Architecture string `json:"architecture"`
	// Repository is the name of the repository it comes from.
	Repository string `json:"repository"`
	URL        string `json:"url"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	// SignedBy is the fingerprint of the key that signed an RPM package.
	SignedBy string `json:"signedBy,omitempty"`
	// Why is the chain of packages that need it, from the nearest to the
	// listed one. It is empty for a listed package.
	Why []string `json:"why,omitempty"`
}
