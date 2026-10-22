package v1alpha1

import (
	"encoding/json"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
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

func TestByteSizeJSON(t *testing.T) {
	var b ByteSize
	if err := json.Unmarshal([]byte(`4096`), &b); err != nil || b != 4*KiB {
		t.Errorf("unmarshal number = %d, %v; want 4096", b, err)
	}
	if err := json.Unmarshal([]byte(`-1`), &b); err == nil {
		t.Error("unmarshal -1 succeeded, want error")
	}
	out, err := json.Marshal(4 * GiB)
	if err != nil || string(out) != `"4GiB"` {
		t.Errorf("marshal = %s, %v; want \"4GiB\"", out, err)
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

func TestDurationJSON(t *testing.T) {
	var d Duration
	if err := json.Unmarshal([]byte(`"72h"`), &d); err != nil || d.Hours() != 72 {
		t.Errorf("unmarshal = %v, %v; want 72h", d, err)
	}
	if err := json.Unmarshal([]byte(`72`), &d); err == nil || !strings.Contains(err.Error(), "must be a string") {
		t.Errorf("unmarshal number error = %v, want a hint to use a string", err)
	}
	out, err := json.Marshal(d)
	if err != nil || string(out) != `"72h0m0s"` {
		t.Errorf("marshal = %s, %v", out, err)
	}
}

// Short forms must survive a round trip, so printed manifests stay readable.
func TestMarshalShortForms(t *testing.T) {
	in := header + `spec:
  targets:
    - distro: ubuntu-24.04
    - distro: {name: rocky-9, components: [baseos]}
  packages:
    - curl
    - {name: kubelet, version: "1.34"}
`
	r, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	out, err := yaml.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"- distro: ubuntu-24.04\n",
		"- curl\n",
		"name: rocky-9",
		`version: "1.34"`,
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	again, err := Parse(out)
	if err != nil {
		t.Fatalf("printed manifest doesn't parse: %v\n%s", err, out)
	}
	if again.Spec.Packages[1].Version != "1.34" {
		t.Errorf("version after round trip = %q, want \"1.34\"", again.Spec.Packages[1].Version)
	}
}
