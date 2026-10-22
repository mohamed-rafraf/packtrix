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

// Package manifest reads and writes Packtrix manifests. A manifest in any
// supported API version is decoded, defaulted with that version's defaults,
// and converted to the internal API.
package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"github.com/packtrix/packtrix/pkg/apis/packtrix"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/scheme"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/validation"
)

// kind is the kind of manifest Packtrix reads.
var kind = packtrix.Kind("OfflineRepository")

// Parse decodes a YAML manifest, applies the defaults of its API version and
// converts it to the internal API. It rejects unknown and duplicate fields,
// but doesn't validate; LoadFile does.
func Parse(data []byte) (*packtrix.OfflineRepository, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("the manifest is empty")
	}
	var meta metav1.TypeMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, decodeError(err)
	}
	versioned, err := newVersioned(meta)
	if err != nil {
		return nil, err
	}
	if err := yaml.UnmarshalStrict(data, versioned); err != nil {
		return nil, decodeError(err)
	}
	scheme.Scheme.Default(versioned)
	r := &packtrix.OfflineRepository{}
	if err := scheme.Scheme.Convert(versioned, r, nil); err != nil {
		return nil, err
	}
	return r, nil
}

// LoadFile reads the manifest at path, applies defaults and validates it.
func LoadFile(path string) (*packtrix.OfflineRepository, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := validation.ValidateOfflineRepository(r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}

// Marshal writes the manifest as YAML in the preferred API version.
func Marshal(r *packtrix.OfflineRepository) ([]byte, error) {
	versioned, err := scheme.Scheme.ConvertToVersion(r, preferredVersion())
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(versioned)
}

// newVersioned returns an empty object of the manifest's API version, or an
// error naming the supported versions.
func newVersioned(meta metav1.TypeMeta) (runtime.Object, error) {
	gvk := meta.GroupVersionKind()
	if gvk.GroupKind() == kind && gvk.Version != runtime.APIVersionInternal {
		if obj, err := scheme.Scheme.New(gvk); err == nil {
			return obj, nil
		}
	}
	return nil, fmt.Errorf("unsupported manifest (apiVersion %q, kind %q): expected apiVersion %q and kind %q",
		meta.APIVersion, meta.Kind, preferredVersion(), kind.Kind)
}

func preferredVersion() schema.GroupVersion {
	return scheme.Scheme.PrioritizedVersionsForGroup(packtrix.GroupName)[0]
}

// decodeError removes the YAML-to-JSON plumbing from decoder messages, which
// would only confuse someone editing a YAML file.
func decodeError(err error) error {
	msg := err.Error()
	for _, prefix := range []string{"error converting YAML to JSON: ", "error unmarshaling JSON: ", "while decoding JSON: ", "json: "} {
		msg = strings.TrimPrefix(msg, prefix)
	}
	return errors.New(msg)
}
