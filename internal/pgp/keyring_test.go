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

package pgp

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The fixtures are real files, fetched on 2026-10-24:
//
//   - ubuntu-noble-security.InRelease, from security.ubuntu.com, signed by
//     the Ubuntu Archive Automatic Signing Key (2018);
//   - ubuntu-archive-keyring.gpg, from Ubuntu's ubuntu-keyring package: the
//     2012 and 2018 archive keys, and the CD image key;
//   - debian-bookworm-security.InRelease, from security.debian.org, signed by
//     subkeys of the Debian 11 and 12 security archive keys;
//   - debian-archive-key-12-security.asc, from ftp-master.debian.org, whose
//     fingerprint ftp-master.debian.org/keys.html publishes.
const (
	ubuntu2018Key     = "F6EC B376 2474 EDA9 D21B  7022 8719 20D1 991B C93C"
	ubuntu2012Key     = "790BC7277767219C42C86F933B4FE6ACC0B21F32"
	debian12Security  = "05AB90340C0C5E797F44A8C8254CF3B5AEC0A8F0"
	debian11SubkeyFpr = "ED541312A33F1128F10B1C6C54404762BBB6E853"
	debian12SubkeyFpr = "B0CAB9266E8C3929798B3EEEBDE6D2B9216EC7A8"
)

// at makes keys be checked at t for the rest of the test.
func at(t *testing.T, tm time.Time) {
	t.Helper()
	saved := now
	now = func() time.Time { return tm }
	t.Cleanup(func() { now = saved })
}

// fixtureTime is after the fixtures were signed and before their keys
// expire.
var fixtureTime = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func keyring(t *testing.T, name string, pinned ...string) *Keyring {
	t.Helper()
	k, err := NewKeyring(readFixture(t, name), pinned...)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestVerifyRealInRelease(t *testing.T) {
	at(t, fixtureTime)
	for _, tt := range []struct {
		inRelease string
		keyring   *Keyring
		signer    string
		codename  string
	}{
		{"ubuntu-noble-security.InRelease", keyring(t, "ubuntu-archive-keyring.gpg", ubuntu2018Key), "F6ECB3762474EDA9D21B7022871920D1991BC93C", "Codename: noble"},
		// Signed by two keys, one of which is pinned.
		{"debian-bookworm-security.InRelease", keyring(t, "debian-archive-key-12-security.asc", debian12Security), debian12Security, "Codename: bookworm-security"},
		// Without pinned fingerprints, every key of the file is trusted.
		{"ubuntu-noble-security.InRelease", keyring(t, "ubuntu-archive-keyring.gpg"), "F6ECB3762474EDA9D21B7022871920D1991BC93C", "Codename: noble"},
	} {
		message := readFixture(t, tt.inRelease)
		text, signer, err := tt.keyring.VerifyClearsigned(message)
		if err != nil {
			t.Errorf("%s: %v", tt.inRelease, err)
			continue
		}
		if signer != tt.signer {
			t.Errorf("%s: signer = %s, want %s", tt.inRelease, signer, tt.signer)
		}
		if !bytes.HasPrefix(text, []byte("Origin: ")) || !bytes.Contains(text, []byte("\n"+tt.codename+"\n")) || bytes.Contains(text, []byte("PGP")) {
			t.Errorf("%s: the text isn't the Release file: %.80q", tt.inRelease, text)
		}

		// Changing any byte breaks the signature: in the text, or in the
		// signature.
		for _, i := range []int{bytes.Index(message, []byte("SHA256:")) + 200, len(message) - 200} {
			tampered := bytes.Clone(message)
			tampered[i] ^= 0x01
			if _, _, err := tt.keyring.VerifyClearsigned(tampered); err == nil {
				t.Errorf("%s: verifies after changing byte %d", tt.inRelease, i)
			}
		}
	}
}

func TestUnpinnedKeys(t *testing.T) {
	at(t, fixtureTime)
	for _, tt := range []struct {
		inRelease string
		keyring   *Keyring
		want      string
	}{
		{"ubuntu-noble-security.InRelease", keyring(t, "ubuntu-archive-keyring.gpg", ubuntu2012Key),
			"signed by key F6ECB3762474EDA9D21B7022871920D1991BC93C, which isn't pinned"},
		{"debian-bookworm-security.InRelease", keyring(t, "ubuntu-archive-keyring.gpg"),
			"signed by keys " + debian11SubkeyFpr + ", " + debian12SubkeyFpr + ", none of which is pinned"},
	} {
		_, _, err := tt.keyring.VerifyClearsigned(readFixture(t, tt.inRelease))
		if err == nil || err.Error() != tt.want {
			t.Errorf("%s: error = %v, want %q", tt.inRelease, err, tt.want)
		}
	}
}

func TestClearsignedFraming(t *testing.T) {
	at(t, fixtureTime)
	k := keyring(t, "ubuntu-archive-keyring.gpg", ubuntu2018Key)
	message := readFixture(t, "ubuntu-noble-security.InRelease")
	end := bytes.Index(message, []byte("-----BEGIN PGP SIGNATURE-----"))
	for _, tt := range []struct {
		name    string
		message []byte
		want    string
	}{
		{"text before", append([]byte("Origin: Evil\n"), message...), "not a clearsigned message: it doesn't start with -----BEGIN PGP SIGNED MESSAGE-----"},
		{"text after", append(bytes.Clone(message), "Origin: Evil\n"...), "unsigned data after the clearsigned message"},
		{"no signature", message[:end], "malformed clearsigned message"},
		{"not armored", append(bytes.Clone(message[:end]), "-----BEGIN PGP SIGNATURE-----\n\n!!!\n-----END PGP SIGNATURE-----\n"...), "reading the signature: "},
	} {
		_, _, err := k.VerifyClearsigned(tt.message)
		if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
			t.Errorf("%s: error = %v, want %q", tt.name, err, tt.want)
		}
	}
	// Whitespace after the message is fine.
	if _, _, err := k.VerifyClearsigned(append(bytes.Clone(message), "\n\n"...)); err != nil {
		t.Errorf("trailing newlines: %v", err)
	}
}

func TestNewKeyring(t *testing.T) {
	k := keyring(t, "ubuntu-archive-keyring.gpg", ubuntu2018Key, strings.ToLower(ubuntu2012Key))
	if got, want := k.Fingerprints(), []string{"F6ECB3762474EDA9D21B7022871920D1991BC93C", ubuntu2012Key}; !reflect.DeepEqual(got, want) {
		t.Errorf("Fingerprints = %q, want %q", got, want)
	}
	if got := keyring(t, "ubuntu-archive-keyring.gpg").Fingerprints(); len(got) != 3 {
		t.Errorf("Fingerprints = %q, want the 3 keys of the file", got)
	}

	for _, tt := range []struct {
		data   []byte
		pinned string
		want   string
	}{
		{readFixture(t, "ubuntu-archive-keyring.gpg"), debian12Security, "the keys don't include the pinned key " + debian12Security},
		{readFixture(t, "ubuntu-archive-keyring.gpg"), "F6ECB376", `invalid fingerprint "F6ECB376"`},
		{[]byte("not a key"), "", "reading keys: "},
		{[]byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\n\n!!!\n-----END PGP PUBLIC KEY BLOCK-----\n"), "", "reading keys: "},
	} {
		var pinned []string
		if tt.pinned != "" {
			pinned = append(pinned, tt.pinned)
		}
		_, err := NewKeyring(tt.data, pinned...)
		if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
			t.Errorf("NewKeyring(%.20q, %q) = %v, want %q", tt.data, tt.pinned, err, tt.want)
		}
	}
}
