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

// Package scheme registers every version of the Packtrix manifest API.
package scheme

import (
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/packtrix/packtrix/pkg/apis/packtrix"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/v1alpha1"
)

// Scheme knows the internal API and every versioned API, with their defaults
// and conversions.
var Scheme = runtime.NewScheme()

func init() {
	AddToScheme(Scheme)
}

// AddToScheme registers the internal API and every versioned API. Manifests
// are written in the first version given to SetVersionPriority.
func AddToScheme(scheme *runtime.Scheme) {
	utilruntime.Must(packtrix.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(scheme.SetVersionPriority(v1alpha1.SchemeGroupVersion))
}
