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

// +k8s:deepcopy-gen=package
// +k8s:conversion-gen=github.com/packtrix/packtrix/pkg/apis/packtrix
// +k8s:defaulter-gen=TypeMeta
// +groupName=packtrix.io

// Package v1alpha1 is version v1alpha1 of the Packtrix manifest
// (apiVersion: packtrix.io/v1alpha1, kind: OfflineRepository): its format and
// its defaults. Manifests are converted to the internal API in package
// packtrix before use.
//
// The types follow Kubernetes API conventions, so the same definitions can
// later back a Kubernetes custom resource.
package v1alpha1
