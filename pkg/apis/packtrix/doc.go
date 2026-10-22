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
// +groupName=packtrix.io

// Package packtrix is the internal version of the Packtrix manifest API.
//
// Manifests are written in a versioned API, such as v1alpha1, and converted to
// these types after defaulting. The rest of Packtrix only works with these
// types. They are never read from or written to files, so they can change
// without breaking manifests.
package packtrix
