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
	"crypto"
	_ "crypto/sha1" // for the SHA-1 signature that TestSHA1Rejected makes
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
)

// t0 is when the test keys are created.
var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func timeAt(tm time.Time) func() time.Time {
	return func() time.Time { return tm }
}

// newKey generates a key at t0.
func newKey(t *testing.T, cfg packet.Config) *openpgp.Entity {
	t.Helper()
	cfg.Time = timeAt(t0)
	e, err := openpgp.NewEntity("Packtrix Test", "", "test@packtrix.io", &cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

var (
	ed25519Key = packet.Config{Algorithm: packet.PubKeyAlgoEdDSA}
	rsa3072Key = packet.Config{Algorithm: packet.PubKeyAlgoRSA, RSABits: 3072}
)

// export returns e's private key, armored, or its public key when public is
// set.
func export(t *testing.T, public bool, entities ...*openpgp.Entity) []byte {
	t.Helper()
	blockType := openpgp.PrivateKeyType
	if public {
		blockType = openpgp.PublicKeyType
	}
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, blockType, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entities {
		if public {
			err = e.Serialize(w)
		} else {
			err = e.SerializePrivateWithoutSigning(w, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func dearmor(t *testing.T, data []byte) []byte {
	t.Helper()
	block, err := armor.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(block.Body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const release = `Origin: Packtrix
Label: k8s-node-prereqs
Suite: noble
- a line that starts with a dash
From a line that starts with From
SHA256:
 4f2f0c3b1b2e8f9a 1234 main/binary-amd64/Packages
`

func TestSignAndVerify(t *testing.T) {
	at(t, t0.Add(time.Hour))
	for name, cfg := range map[string]packet.Config{"Ed25519": ed25519Key, "RSA-3072": rsa3072Key} {
		t.Run(name, func(t *testing.T) {
			s, err := LoadSigner(export(t, false, newKey(t, cfg)))
			if err != nil {
				t.Fatal(err)
			}
			public, err := s.PublicKey()
			if err != nil {
				t.Fatal(err)
			}
			k, err := NewKeyring(public, s.Fingerprint())
			if err != nil {
				t.Fatal(err)
			}

			inRelease, err := s.Clearsign([]byte(release))
			if err != nil {
				t.Fatal(err)
			}
			text, signer, err := k.VerifyClearsigned(inRelease)
			if err != nil {
				t.Fatal(err)
			}
			// The newline before the signature isn't part of the signed text.
			if string(text) != strings.TrimSuffix(release, "\n") || signer != s.Fingerprint() {
				t.Errorf("verified %q by %s, want the Release file by %s", text, signer, s.Fingerprint())
			}

			sig, err := s.DetachSign([]byte(release))
			if err != nil {
				t.Fatal(err)
			}
			for _, sig := range [][]byte{sig, dearmor(t, sig)} {
				if signer, err := k.VerifyDetached([]byte(release), sig); err != nil || signer != s.Fingerprint() {
					t.Errorf("VerifyDetached = %s, %v, want %s", signer, err, s.Fingerprint())
				}
			}
			if _, err := k.VerifyDetached([]byte(release+"Extra: field\n"), sig); err == nil || !strings.HasPrefix(err.Error(), "bad signature: ") {
				t.Errorf("VerifyDetached of changed data = %v, want a bad signature", err)
			}

			gpgv(t, dearmor(t, public), inRelease, sig)
		})
	}
}

// gpgv checks that the gpgv of apt's machines accepts Packtrix's signatures.
func gpgv(t *testing.T, publicKey, inRelease, releaseGPG []byte) {
	t.Helper()
	bin, err := exec.LookPath("gpgv")
	if err != nil {
		t.Log("gpgv isn't installed; skipping the check with gpgv")
		return
	}
	dir := t.TempDir()
	files := map[string][]byte{"key.gpg": publicKey, "InRelease": inRelease, "Release": []byte(release), "Release.gpg": releaseGPG}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"InRelease"}, {"Release.gpg", "Release"}} {
		cmd := exec.CommandContext(t.Context(), bin, append([]string{"--homedir", dir, "--keyring", filepath.Join(dir, "key.gpg")}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("gpgv %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

func TestLoadSignerRejects(t *testing.T) {
	at(t, t0.Add(time.Hour))
	encrypted := newKey(t, ed25519Key)
	if err := encrypted.EncryptPrivateKeys([]byte("passphrase"), nil); err != nil {
		t.Fatal(err)
	}
	expiring := ed25519Key
	expiring.KeyLifetimeSecs = 60

	for _, tt := range []struct {
		name string
		key  func() *openpgp.Entity
		data []byte
		want string
	}{
		{name: "RSA-2048", key: func() *openpgp.Entity {
			return newKey(t, packet.Config{Algorithm: packet.PubKeyAlgoRSA, RSABits: 2048})
		}, want: "can't sign repositories: RSA keys must have at least 3072 bits, not 2048"},
		{name: "ECDSA P-256", key: func() *openpgp.Entity {
			return newKey(t, packet.Config{Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP256})
		}, want: "can't sign repositories: use RSA of at least 3072 bits or Ed25519"},
		{name: "version 6", key: func() *openpgp.Entity {
			return newKey(t, packet.Config{Algorithm: packet.PubKeyAlgoEd25519, V6Keys: true})
		}, want: "can't sign repositories: use a version 4 key, not version 6, which gpgv can't verify"},
		{name: "encrypted", key: func() *openpgp.Entity { return encrypted }, want: "is protected by a passphrase, which isn't supported yet"},
		{name: "expired", key: func() *openpgp.Entity { return newKey(t, expiring) }, want: "has expired"},
		{name: "public key", data: export(t, true, newKey(t, ed25519Key)), want: "is a public key; the signing key must be a private key"},
		{name: "two keys", data: export(t, false, newKey(t, ed25519Key), newKey(t, ed25519Key)), want: "the signing key file must hold one key, not 2"},
		{name: "not a key", data: []byte("not a key"), want: "reading the signing key: "},
	} {
		data := tt.data
		if tt.key != nil {
			data = export(t, false, tt.key())
		}
		_, err := LoadSigner(data)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error = %v, want %q", tt.name, err, tt.want)
		}
	}
}

// apt rejects a signature by a key that has expired or been revoked since it
// signed, and so does Packtrix.
func TestKeyNoLongerValid(t *testing.T) {
	expiring := ed25519Key
	expiring.KeyLifetimeSecs = 3600
	revoked := newKey(t, ed25519Key)
	withSubkey := newKey(t, ed25519Key)
	if err := withSubkey.AddSigningSubkey(&packet.Config{Algorithm: packet.PubKeyAlgoEdDSA, KeyLifetimeSecs: 3600, Time: timeAt(t0)}); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name   string
		key    *openpgp.Entity
		revoke bool
		want   string
	}{
		{"expired", newKey(t, expiring), false, "which has expired"},
		{"revoked", revoked, true, "which has been revoked"},
		{"expired subkey", withSubkey, false, "which has expired"},
	} {
		at(t, t0.Add(10*time.Minute))
		s, err := LoadSigner(export(t, false, tt.key))
		if err != nil {
			t.Fatal(err)
		}
		sig, err := s.DetachSign([]byte(release))
		if err != nil {
			t.Fatal(err)
		}
		if tt.revoke {
			if err := tt.key.Revoke(packet.KeyCompromised, "", &packet.Config{Time: timeAt(t0.Add(20 * time.Minute))}); err != nil {
				t.Fatal(err)
			}
		}
		k, err := NewKeyring(export(t, true, tt.key))
		if err != nil {
			t.Fatal(err)
		}

		at(t, t0.Add(2*time.Hour))
		_, err = k.VerifyDetached([]byte(release), sig)
		if err == nil || !strings.HasPrefix(err.Error(), "signed by key ") || !strings.HasSuffix(err.Error(), tt.want) {
			t.Errorf("%s: error = %v, want %q", tt.name, err, tt.want)
		}
	}
}

func TestSHA1Rejected(t *testing.T) {
	at(t, t0.Add(time.Hour))
	e := newKey(t, rsa3072Key)
	// The library won't make a SHA-1 signature, so make the packet by hand.
	sig := &packet.Signature{
		SigType:      packet.SigTypeBinary,
		PubKeyAlgo:   e.PrivateKey.PubKeyAlgo,
		Hash:         crypto.SHA1,
		CreationTime: now(),
		IssuerKeyId:  &e.PrimaryKey.KeyId,
	}
	h, err := sig.PrepareSign(nil)
	if err != nil {
		t.Fatal(err)
	}
	h.Write([]byte(release))
	if err := sig.Sign(h, e.PrivateKey, config()); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := sig.Serialize(&buf); err != nil {
		t.Fatal(err)
	}

	k, err := NewKeyring(export(t, true, e))
	if err != nil {
		t.Fatal(err)
	}
	_, err = k.VerifyDetached([]byte(release), buf.Bytes())
	if want := "bad signature: openpgp: invalid signature: insecure message hash algorithm: SHA-1"; err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func TestVerifyDetachedErrors(t *testing.T) {
	at(t, t0.Add(time.Hour))
	e := newKey(t, ed25519Key)
	k, err := NewKeyring(export(t, true, e))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, signature, want string
	}{
		{"not armored", "-----BEGIN PGP SIGNATURE-----\n\n!!!\n-----END PGP SIGNATURE-----\n", "reading the signature: "},
		{"a key", string(export(t, true, e)), "bad signature: "},
		{"empty", "", "bad signature: no signature packet"},
	} {
		_, err := k.VerifyDetached([]byte(release), []byte(tt.signature))
		if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
			t.Errorf("%s: error = %v, want %q", tt.name, err, tt.want)
		}
	}
}
