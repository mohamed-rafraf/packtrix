package v1alpha1

import (
	"bytes"
	"encoding/json"
	"errors"
)

// UnmarshalJSON accepts a package name ("curl") or an object.
func (p *Package) UnmarshalJSON(data []byte) error {
	switch firstByte(data) {
	case '"':
		var name string
		if err := json.Unmarshal(data, &name); err != nil {
			return err
		}
		*p = Package{Name: name}
		return nil
	case '{':
		type plain Package
		return decodeStrict(data, (*plain)(p))
	default:
		return errors.New("a package must be a name or an object")
	}
}

// MarshalJSON writes a package that only has a name in its short form.
func (p Package) MarshalJSON() ([]byte, error) {
	if p.Version == "" && p.Deb == "" && p.RPM == "" && len(p.Only) == 0 && len(p.Except) == 0 && p.Repo == "" {
		return json.Marshal(p.Name)
	}
	type plain Package
	return json.Marshal(plain(p))
}

// UnmarshalJSON accepts a distribution name ("ubuntu-24.04") or an object.
func (d *Distro) UnmarshalJSON(data []byte) error {
	switch firstByte(data) {
	case '"':
		var name string
		if err := json.Unmarshal(data, &name); err != nil {
			return err
		}
		*d = Distro{Name: name}
		return nil
	case '{':
		type plain Distro
		return decodeStrict(data, (*plain)(d))
	default:
		return errors.New("distro must be a name or an object")
	}
}

// MarshalJSON writes a distro that only has a name in its short form.
func (d Distro) MarshalJSON() ([]byte, error) {
	if len(d.Components) == 0 && d.Mirror == "" && d.Auth == nil {
		return json.Marshal(d.Name)
	}
	type plain Distro
	return json.Marshal(plain(d))
}

// decodeStrict decodes data into v and rejects unknown fields. Custom
// unmarshalers need it because the strictness of the outer decoder doesn't
// reach them.
func decodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func firstByte(data []byte) byte {
	data = bytes.TrimLeft(data, " \t\r\n")
	if len(data) == 0 {
		return 0
	}
	return data[0]
}
