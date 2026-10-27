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

// OfflineRepository describes which machines an offline environment runs and
// which packages they need. Packtrix turns it into one signed repository per
// target.
type OfflineRepository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec OfflineRepositorySpec `json:"spec"`
}

// OfflineRepositorySpec is the desired content of the offline repositories.
type OfflineRepositorySpec struct {
	// Targets are the kinds of machines to build repositories for.
	Targets []Target `json:"targets"`
	// Packages are written once and resolved, with dependencies, for every
	// target they apply to.
	Packages []Package `json:"packages"`
	// AutoRepos controls automatic detection of built-in vendor repositories.
	AutoRepos AutoRepos `json:"autoRepos,omitzero"`
	// Resolve controls dependency resolution.
	Resolve Resolve `json:"resolve,omitzero"`
	// Updates controls how upstream updates are picked up.
	Updates Updates `json:"updates,omitzero"`
	// Publish controls where and how repositories are written.
	Publish Publish `json:"publish,omitzero"`
	// Export controls how bundles are written.
	Export Export `json:"export,omitzero"`
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
// architectures. Exactly one of Distro or Repos must be set.
type Target struct {
	// Name identifies the target in only/except and in output paths.
	// Defaults to the distro name.
	Name string `json:"name,omitempty"`
	// Distro is a built-in distribution, e.g. "ubuntu-24.04".
	Distro *Distro `json:"distro,omitempty"`
	// Architectures defaults to [amd64].
	Architectures []string `json:"architectures,omitempty"`
	// ExtraRepos are your own repositories, added on top of Distro.
	ExtraRepos []Repository `json:"extraRepos,omitempty"`
	// Repos is the complete repository list of a distribution Packtrix
	// doesn't know. Vendor repositories aren't detected for these targets.
	Repos []Repository `json:"repos,omitempty"`
}

// Distro selects a built-in distribution. In YAML it is either a name
// ("ubuntu-24.04") or an object that changes the distribution's defaults.
type Distro struct {
	// Name is the built-in distribution, e.g. "ubuntu-24.04".
	Name string `json:"name"`
	// Components selects parts of the distribution, e.g. [main, universe]
	// or [baseos, appstream, crb]. Defaults depend on the distribution.
	Components []string `json:"components,omitempty"`
	// Mirror replaces the official servers, e.g. with a Nexus or
	// Artifactory proxy. Paths and keys stay the same.
	Mirror string `json:"mirror,omitempty"`
	// Auth holds credentials for distributions that need them, such as RHEL.
	Auth *Auth `json:"auth,omitempty"`
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
	// Name identifies the repository in plans, locks and paths.
	Name string `json:"name"`
	// Type is the repository's package format: "apt" or "rpm".
	Type RepoType `json:"type"`
	// URL is the repository's base URL. {arch} is replaced with the
	// architecture's native name.
	URL string `json:"url"`
	// Suite is required for apt repositories, e.g. "stable", or "/" for a
	// flat repository.
	Suite string `json:"suite,omitempty"`
	// Components is required for apt repositories that aren't flat.
	Components []string `json:"components,omitempty"`
	// GPGKey is the path to the repository's public signing key.
	GPGKey string `json:"gpgKey"`
	// Auth holds credentials, when the repository needs them.
	Auth *Auth `json:"auth,omitempty"`
}

// Auth holds credentials for an upstream repository. Secrets are always
// references, never literal values.
type Auth struct {
	// Username and Password are sent with basic authentication. Password is
	// a secret reference: "env:NAME" or "file:/path".
	Username string    `json:"username,omitempty"`
	Password SecretRef `json:"password,omitempty"`
	// Token is sent as a bearer token. It is a secret reference: "env:NAME"
	// or "file:/path".
	Token SecretRef `json:"token,omitempty"`
	// ClientCert and ClientKey are paths to a TLS client certificate.
	ClientCert string `json:"clientCert,omitempty"`
	ClientKey  string `json:"clientKey,omitempty"`
	// Entitlement is the directory holding RHEL subscription certificates,
	// usually /etc/pki/entitlement.
	Entitlement string `json:"entitlement,omitempty"`
}

// Package is one entry of the package list. In YAML it is either a name
// ("curl") or an object.
type Package struct {
	// Name is the package name. When Deb or RPM is set, it is only a label.
	Name string `json:"name"`
	// Version constrains the version, e.g. "1.34.*". It also selects the
	// repository of vendors that publish one repository per version.
	Version VersionConstraint `json:"version,omitempty"`
	// Deb and RPM are the real package names for one format, when they
	// differ from Name.
	Deb string `json:"deb,omitempty"`
	RPM string `json:"rpm,omitempty"`
	// Only limits the package to these targets. Values are target names,
	// or "deb"/"rpm" for every target of that format.
	Only []string `json:"only,omitempty"`
	// Except applies the package to every target but these.
	Except []string `json:"except,omitempty"`
	// Repo takes the package from this built-in vendor repository instead
	// of the distribution, e.g. "nginx-org".
	Repo string `json:"repo,omitempty"`
}

// Selectors that match every target of a package format in Only/Except.
const (
	SelectorDeb = "deb"
	SelectorRPM = "rpm"
)

// AutoRepos controls automatic detection of built-in vendor repositories.
type AutoRepos struct {
	// Enabled defaults to true.
	Enabled *bool `json:"enabled,omitempty"`
	// Deny lists repositories never to add automatically, as glob patterns,
	// e.g. "epel-*".
	Deny []string `json:"deny,omitempty"`
}

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
	// Baseline is what machines are assumed to have already: "minimal"
	// (default), "none", or "lock:<path>" for a discovered host.
	Baseline string `json:"baseline,omitempty"`
	// Recommends includes APT Recommends and RPM weak dependencies.
	Recommends bool `json:"recommends,omitempty"`
	// Alternatives picks "first" (default) or "all" branches of APT "a | b"
	// dependencies.
	Alternatives string `json:"alternatives,omitempty"`
	// Exclude lists packages never to download.
	Exclude []string `json:"exclude,omitempty"`
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
	Check string `json:"check,omitempty"`
	// Accept is "all" (default), "security", or "none".
	Accept string `json:"accept,omitempty"`
	// MinAge is how long a new version must have been published before it
	// is taken, e.g. "72h".
	MinAge Duration `json:"minAge,omitzero"`
	// Publish is "approval" (default: stop after plan) or "auto".
	Publish string `json:"publish,omitempty"`
}

// Publish controls where and how repositories are written.
type Publish struct {
	// Path is the output directory. Defaults to "./repos".
	Path    string  `json:"path,omitempty"`
	Signing Signing `json:"signing,omitzero"`
	// KeepSnapshots is the number of earlier snapshots kept for rollback.
	// Defaults to 3.
	KeepSnapshots *int `json:"keepSnapshots,omitempty"`
}

// Signing configures the key that signs published repository metadata.
type Signing struct {
	// Key references the OpenPGP private key: "env:NAME" reads an
	// environment variable, and "file:/path" a file.
	Key SecretRef `json:"key,omitempty"`
}

// Export formats.
const ExportFormatTarZst = "tar.zst"

// Export controls how bundles are written.
type Export struct {
	// Format defaults to "tar.zst".
	Format string `json:"format,omitempty"`
	// SplitSize splits bundles into parts of this size, e.g. "4GiB".
	SplitSize ByteSize `json:"splitSize,omitempty"`
}
