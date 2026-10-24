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
	"errors"
	"fmt"
	"io"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
)

// minRSABits is the smallest RSA key that signs published repositories.
const minRSABits = 3072

// Signer signs the published repositories.
type Signer struct {
	entity *openpgp.Entity
}

// LoadSigner reads a private key, armored or binary, such as the one that
// publish.signing.key references. Its signing key must be valid now, not
// protected by a passphrase, and either RSA of at least 3072 bits or Ed25519,
// in a version 4 key: the gpgv that apt runs can't verify version 6
// signatures.
func LoadSigner(data []byte) (*Signer, error) {
	entities, err := readEntities(data)
	if err != nil {
		return nil, fmt.Errorf("reading the signing key: %w", err)
	}
	if len(entities) != 1 {
		return nil, fmt.Errorf("the signing key file must hold one key, not %d", len(entities))
	}
	s := &Signer{entity: entities[0]}
	key, err := s.key()
	if err != nil {
		return nil, err
	}
	if key.PrivateKey == nil {
		return nil, fmt.Errorf("key %s is a public key; the signing key must be a private key", s.Fingerprint())
	}
	if key.PrivateKey.Encrypted {
		return nil, fmt.Errorf("key %s is protected by a passphrase, which isn't supported yet", s.Fingerprint())
	}
	if err := checkStrength(key.PublicKey); err != nil {
		return nil, fmt.Errorf("key %s can't sign repositories: %w", s.Fingerprint(), err)
	}
	return s, nil
}

// key returns the key that signs now: a signing subkey, or the primary key.
func (s *Signer) key() (openpgp.Key, error) {
	if _, err := s.entity.VerifyPrimaryKey(now(), config()); err != nil {
		return openpgp.Key{}, fmt.Errorf("key %s %s", s.Fingerprint(), unusable(err))
	}
	key, ok := s.entity.SigningKey(now(), config())
	if !ok {
		return openpgp.Key{}, fmt.Errorf("key %s has no valid signing key", s.Fingerprint())
	}
	return key, nil
}

func checkStrength(pk *packet.PublicKey) error {
	if pk.Version != 4 {
		return fmt.Errorf("use a version 4 key, not version %d, which gpgv can't verify", pk.Version)
	}
	switch pk.PubKeyAlgo {
	case packet.PubKeyAlgoRSA, packet.PubKeyAlgoRSASignOnly:
		bits, err := pk.BitLength()
		if err != nil {
			return err
		}
		if bits < minRSABits {
			return fmt.Errorf("RSA keys must have at least %d bits, not %d", minRSABits, bits)
		}
		return nil
	case packet.PubKeyAlgoEdDSA:
		if curve, err := pk.Curve(); err == nil && curve == packet.Curve25519 {
			return nil
		}
	}
	return errors.New("use RSA of at least 3072 bits or Ed25519")
}

// Fingerprint returns the fingerprint of the signing key's primary key, which
// machines pin.
func (s *Signer) Fingerprint() string {
	return fingerprint(s.entity.PrimaryKey)
}

// PublicKey returns the armored public key, for the machines to install.
func (s *Signer) PublicKey() ([]byte, error) {
	var buf bytes.Buffer
	if err := s.entity.Serialize(&buf); err != nil {
		return nil, err
	}
	return armored(openpgp.PublicKeyType, buf.Bytes())
}

// Clearsign returns text signed as a clearsigned message, such as an
// InRelease file. As with gpg, the newline that ends the text is the one
// before the signature, which isn't part of the signed text.
func (s *Signer) Clearsign(text []byte) ([]byte, error) {
	key, err := s.key()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w, err := clearsign.Encode(&buf, key.PrivateKey, config())
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(bytes.TrimSuffix(text, []byte("\n"))); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	// Armor the signature again, with a checksum.
	out := buf.Bytes()
	i := bytes.Index(out, []byte("\n-----BEGIN PGP SIGNATURE-----")) + 1
	block, err := armor.Decode(bytes.NewReader(out[i:]))
	if err != nil {
		return nil, err
	}
	sig, err := io.ReadAll(block.Body)
	if err != nil {
		return nil, err
	}
	signature, err := armored(openpgp.SignatureType, sig)
	if err != nil {
		return nil, err
	}
	return append(out[:i:i], signature...), nil
}

// DetachSign returns an armored detached signature of data, such as
// Release.gpg or repomd.xml.asc.
func (s *Signer) DetachSign(data []byte) ([]byte, error) {
	if _, err := s.key(); err != nil {
		return nil, err
	}
	var sig bytes.Buffer
	if err := openpgp.DetachSign(&sig, []*openpgp.Entity{s.entity}, bytes.NewReader(data), config()); err != nil {
		return nil, err
	}
	return armored(openpgp.SignatureType, sig.Bytes())
}

// armored encodes data in ASCII armor with a CRC-24 checksum, and a newline at
// the end. go-crypto leaves the checksum out of signatures, as RFC 9580
// allows, but gpgv then misreads a signature whose last line has no padding.
func armored(blockType string, data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, blockType, nil)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return append(buf.Bytes(), '\n'), nil
}
