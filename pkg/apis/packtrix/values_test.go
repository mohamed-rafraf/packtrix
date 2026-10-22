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
	"testing"
)

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		in      string
		want    ByteSize
		wantErr bool
	}{
		{in: "4GiB", want: 4 * GiB},
		{in: "512MiB", want: 512 * MiB},
		{in: "1.5GiB", want: 3 * GiB / 2},
		{in: "4 GiB", want: 4 * GiB},
		{in: "500MB", want: 500_000_000},
		{in: "1TB", want: 1_000_000_000_000},
		{in: "10B", want: 10},
		{in: "4", wantErr: true},
		{in: "GiB", wantErr: true},
		{in: "4gib", wantErr: true},
		{in: "4XB", wantErr: true},
		{in: "1.2.3GiB", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseByteSize(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseByteSize(%q) = %d, want error", tt.in, got)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("ParseByteSize(%q) = %d, %v; want %d", tt.in, got, err, tt.want)
		}
	}
}

func TestByteSizeString(t *testing.T) {
	tests := map[ByteSize]string{
		4 * GiB:    "4GiB",
		1536 * MiB: "1536MiB",
		2 * TiB:    "2TiB",
		1000:       "1000B",
		0:          "0B",
	}
	for in, want := range tests {
		if got := in.String(); got != want {
			t.Errorf("ByteSize(%d).String() = %q, want %q", in, got, want)
		}
	}
}

func TestSecretRefValidate(t *testing.T) {
	valid := []SecretRef{"env:PACKTRIX_SIGNING_KEY", "env:_X1", "file:/etc/packtrix/key.asc", "file:key.asc"}
	for _, r := range valid {
		if err := r.Validate(); err != nil {
			t.Errorf("%q: %v", r, err)
		}
	}
	invalid := []SecretRef{"PACKTRIX_KEY", "env:", "env:1X", "env:A-B", "file:", "vault:secret/key", "s3cret"}
	for _, r := range invalid {
		if err := r.Validate(); err == nil {
			t.Errorf("%q: want error", r)
		}
	}
}
