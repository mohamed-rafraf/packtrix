package v1alpha1

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"sigs.k8s.io/yaml"
)

// Parse decodes a YAML manifest and rejects unknown and duplicate fields.
// It doesn't apply defaults or validate; LoadFile does both.
func Parse(data []byte) (*OfflineRepository, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("the manifest is empty")
	}
	var meta TypeMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, decodeError(err)
	}
	if meta.APIVersion != APIVersion || meta.Kind != Kind {
		return nil, fmt.Errorf("unsupported manifest (apiVersion %q, kind %q): expected apiVersion %q and kind %q",
			meta.APIVersion, meta.Kind, APIVersion, Kind)
	}
	var r OfflineRepository
	if err := yaml.UnmarshalStrict(data, &r); err != nil {
		return nil, decodeError(err)
	}
	return &r, nil
}

// LoadFile reads the manifest at path, applies defaults and validates it.
func LoadFile(path string) (*OfflineRepository, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	r.Default()
	if err := r.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
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
