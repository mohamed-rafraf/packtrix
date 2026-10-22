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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// OfflineRepository describes which machines an offline environment runs and
// which packages they need. Packtrix turns it into one signed repository per
// target.
type OfflineRepository struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Spec OfflineRepositorySpec
}

// OfflineRepositorySpec is the desired content of the offline repositories.
type OfflineRepositorySpec struct {
	// Targets are the kinds of machines to build repositories for.
	Targets []Target
	// Packages are resolved, with dependencies, for every target they apply
	// to.
	Packages []Package
	// AutoRepos controls automatic detection of built-in vendor repositories.
	AutoRepos AutoRepos
	// Resolve controls dependency resolution.
	Resolve Resolve
	// Updates controls how upstream updates are picked up.
	Updates Updates
	// Publish controls where and how repositories are written.
	Publish Publish
	// Export controls how bundles are written.
	Export Export
}

// Supported architectures, in Packtrix's normalized form. They are rendered
// as x86_64 and aarch64 for RPM repositories.
const (
	ArchAMD64 = "amd64"
	ArchARM64 = "arm64"
)

// SupportedArchitectures lists the values allowed in Target.Architectures.
var SupportedArchitectures = []string{ArchAMD64, ArchARM64}

// Target is one kind of machine: a distribution, a release and one or more
// architectures. Exactly one of Distro or Repos is set.
type Target struct {
	// Name identifies the target in only/except and in output paths.
	Name string
	// Distro is a built-in distribution.
	Distro *Distro
	// Architectures are the architectures to build repositories for.
	Architectures []string
	// ExtraRepos are your own repositories, added on top of Distro.
	ExtraRepos []Repository
	// Repos is the complete repository list of a distribution Packtrix
	// doesn't know. Vendor repositories aren't detected for these targets.
	Repos []Repository
}

// Distro selects a built-in distribution and changes its defaults.
type Distro struct {
	Name string
	// Components selects parts of the distribution, e.g. [main, universe]
	// or [baseos, appstream, crb]. Empty means the distribution's defaults.
	Components []string
	// Mirror replaces the official servers. Paths and keys stay the same.
	Mirror string
	// Auth holds credentials for distributions that need them, such as RHEL.
	Auth *Auth
}

// RepoType is the package format of a repository.
type RepoType string

// Repository types.
const (
	RepoTypeAPT RepoType = "apt"
	RepoTypeRPM RepoType = "rpm"
)

// Repository is a repository that isn't part of the built-in catalog.
type Repository struct {
	Name string
	Type RepoType
	// URL is the repository's base URL. {arch} is replaced with the
	// architecture's native name.
	URL string
	// Suite is the apt suite, e.g. "stable", or "/" for a flat repository.
	Suite string
	// Components are the apt components of a repository that isn't flat.
	Components []string
	// GPGKey is the path to the repository's public signing key.
	GPGKey string
	Auth   *Auth
}

// Auth holds credentials for an upstream repository. Secrets are always
// references, never literal values.
type Auth struct {
	Username string
	Password SecretRef
	// Token is sent as a bearer token.
	Token SecretRef
	// ClientCert and ClientKey are paths to a TLS client certificate.
	ClientCert string
	ClientKey  string
	// Entitlement is the directory holding RHEL subscription certificates.
	Entitlement string
}

// Package is one entry of the package list.
type Package struct {
	// Name is the package name. When Deb or RPM is set, it is only a label.
	Name string
	// Version constrains the version, e.g. "1.34.*". It also selects the
	// repository of vendors that publish one repository per version.
	Version VersionConstraint
	// Deb and RPM are the real package names for one format, when they
	// differ from Name.
	Deb string
	RPM string
	// Only limits the package to these targets. Values are target names,
	// or "deb"/"rpm" for every target of that format.
	Only []string
	// Except applies the package to every target but these.
	Except []string
	// Repo takes the package from this built-in vendor repository instead
	// of the distribution, e.g. "nginx-org".
	Repo string
}

// Selectors that match every target of a package format in Only/Except.
const (
	SelectorDeb = "deb"
	SelectorRPM = "rpm"
)

// AutoRepos controls automatic detection of built-in vendor repositories.
type AutoRepos struct {
	Enabled *bool
	// Deny lists repositories never to add automatically, as glob patterns,
	// e.g. "epel-*".
	Deny []string
}

// IsEnabled reports whether automatic detection is on.
func (a AutoRepos) IsEnabled() bool { return a.Enabled == nil || *a.Enabled }

// Baseline values. A baseline may also be "lock:<path>".
const (
	BaselineMinimal    = "minimal"
	BaselineNone       = "none"
	BaselineLockPrefix = "lock:"
)

// Alternatives values.
const (
	AlternativesFirst = "first"
	AlternativesAll   = "all"
)

// Resolve controls dependency resolution.
type Resolve struct {
	// Baseline is what machines are assumed to have already: "minimal",
	// "none", or "lock:<path>" for a discovered host.
	Baseline string
	// Recommends includes APT Recommends and RPM weak dependencies.
	Recommends bool
	// Alternatives picks "first" or "all" branches of APT "a | b"
	// dependencies.
	Alternatives string
	// Exclude lists packages never to download.
	Exclude []string
}

// Accept values.
const (
	AcceptAll      = "all"
	AcceptSecurity = "security"
	AcceptNone     = "none"
)

// Publish mode values.
const (
	PublishAuto     = "auto"
	PublishApproval = "approval"
)

// Updates controls how upstream updates are picked up.
type Updates struct {
	// Check is a cron expression for scheduled update checks. Empty means
	// updates are checked manually.
	Check string
	// Accept is "all", "security", or "none".
	Accept string
	// MinAge is how long a new version must have been published before it
	// is taken.
	MinAge time.Duration
	// Publish is "approval" (stop after plan) or "auto".
	Publish string
}

// Publish controls where and how repositories are written.
type Publish struct {
	// Path is the output directory.
	Path    string
	Signing Signing
	// KeepSnapshots is the number of earlier snapshots kept for rollback.
	KeepSnapshots *int
}

// Signing configures the key that signs published repository metadata.
type Signing struct {
	Key SecretRef
}

// Export formats.
const ExportFormatTarZst = "tar.zst"

// Export controls how bundles are written.
type Export struct {
	Format string
	// SplitSize splits bundles into parts of this size. Zero means bundles
	// aren't split.
	SplitSize ByteSize
}
