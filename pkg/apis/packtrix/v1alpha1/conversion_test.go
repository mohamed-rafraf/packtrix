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
	"testing"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/dump"
	"sigs.k8s.io/randfill"

	"github.com/packtrix/packtrix/pkg/apis/packtrix"
)

// Every field must survive a conversion to the internal API and back, in both
// directions, or information in a manifest would be lost.
func TestConversionRoundTrip(t *testing.T) {
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{packtrix.AddToScheme, AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	filler := randfill.New().NilChance(0.3).NumElements(1, 3)

	for range 50 {
		var versioned OfflineRepository
		filler.Fill(&versioned)
		// Conversions don't carry apiVersion and kind; the scheme sets them.
		versioned.TypeMeta = metav1.TypeMeta{}
		roundTrip(t, scheme, &versioned, &packtrix.OfflineRepository{}, &OfflineRepository{})

		var internal packtrix.OfflineRepository
		filler.Fill(&internal)
		internal.TypeMeta = metav1.TypeMeta{}
		roundTrip(t, scheme, &internal, &OfflineRepository{}, &packtrix.OfflineRepository{})
	}
}

// roundTrip converts in to via, then to out, and checks that out equals in.
func roundTrip(t *testing.T, scheme *runtime.Scheme, in, via, out runtime.Object) {
	t.Helper()
	if err := scheme.Convert(in, via, nil); err != nil {
		t.Fatal(err)
	}
	if err := scheme.Convert(via, out, nil); err != nil {
		t.Fatal(err)
	}
	if !apiequality.Semantic.DeepEqual(in, out) {
		t.Errorf("round trip changed the object:\n%s\nwant:\n%s", dump.Pretty(out), dump.Pretty(in))
	}
}
