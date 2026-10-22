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
	"k8s.io/apimachinery/pkg/runtime"
)

// Default values.
const (
	DefaultArchitecture  = ArchAMD64
	DefaultPublishPath   = "./repos"
	DefaultKeepSnapshots = 3
)

func addDefaultingFuncs(scheme *runtime.Scheme) error {
	return RegisterDefaults(scheme)
}

// SetDefaults_OfflineRepository fills in every setting the manifest leaves
// empty. Defaults that depend on the distribution, such as its components,
// come from the built-in catalog instead.
func SetDefaults_OfflineRepository(obj *OfflineRepository) {
	s := &obj.Spec
	if s.AutoRepos.Enabled == nil {
		s.AutoRepos.Enabled = ptr(true)
	}
	if s.Resolve.Baseline == "" {
		s.Resolve.Baseline = BaselineMinimal
	}
	if s.Resolve.Alternatives == "" {
		s.Resolve.Alternatives = AlternativesFirst
	}
	if s.Updates.Accept == "" {
		s.Updates.Accept = AcceptAll
	}
	if s.Updates.Publish == "" {
		s.Updates.Publish = PublishApproval
	}
	if s.Publish.Path == "" {
		s.Publish.Path = DefaultPublishPath
	}
	if s.Publish.KeepSnapshots == nil {
		s.Publish.KeepSnapshots = ptr(DefaultKeepSnapshots)
	}
	if s.Export.Format == "" {
		s.Export.Format = ExportFormatTarZst
	}
}

// SetDefaults_Target names a target after its distro and builds it for amd64
// unless the manifest says otherwise.
func SetDefaults_Target(obj *Target) {
	if obj.Name == "" && obj.Distro != nil {
		obj.Name = obj.Distro.Name
	}
	if len(obj.Architectures) == 0 {
		obj.Architectures = []string{DefaultArchitecture}
	}
}

func ptr[T any](v T) *T { return &v }
