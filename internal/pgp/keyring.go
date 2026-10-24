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

// Package pgp verifies upstream signatures against pinned keys, and signs
// the published repositories. It runs in process, with no gpg binary or
// agent.
package pgp

import (
	"bytes"
	"crypto"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
	pgperrors "github.com/ProtonMail/go-crypto/openpgp/errors"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	openpgp "github.com/ProtonMail/go-crypto/openpgp/v2"
)

// now is the time at which keys must be valid. Tests change it.
var now = time.Now

// config is the policy of every signature that Packtrix checks or makes. The
// library's defaults reject SHA-1, MD5 and RIPEMD-160 message signatures, and
// DSA and ElGamal keys. New signatures use SHA-512, without the random
// notation that the library would add and that older parsers don't expect.
func config() *packet.Config {
	return &packet.Config{Time: now, DefaultHash: crypto.SHA512, NonDeterministicSignaturesViaNotation: new(false)}
}

// unusable describes why a key that VerifyPrimaryKey or Subkey.Verify
// rejected can't be used.
func unusable(err error) string {
	switch {
	case errors.Is(err, pgperrors.ErrKeyExpired):
		return "has expired"
	case errors.Is(err, pgperrors.ErrKeyRevoked):
		return "has been revoked"
	default:
		return "can't be used: " + err.Error()
	}
}

var fingerprintRE = regexp.MustCompile(`^([0-9A-F]{40}|[0-9A-F]{64})$`)

func fingerprint(pk *packet.PublicKey) string {
	return strings.ToUpper(hex.EncodeToString(pk.Fingerprint))
}

// readEntities reads keys, armored or binary. An armored file may hold
// several blocks.
func readEntities(data []byte) (openpgp.EntityList, error) {
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("-----BEGIN PGP")) {
		return openpgp.ReadKeyRing(bytes.NewReader(data))
	}
	var entities openpgp.EntityList
	r := bytes.NewReader(data)
	for {
		block, err := armor.Decode(r)
		if errors.Is(err, io.EOF) {
			return entities, nil
		}
		if err != nil {
			return nil, err
		}
		list, err := openpgp.ReadKeyRing(block.Body)
		if err != nil {
			return nil, err
		}
		entities = append(entities, list...)
	}
}

// Keyring holds the keys trusted to sign one repository.
type Keyring struct {
	entities openpgp.EntityList
}

// NewKeyring reads public keys, armored or binary, and keeps the pinned
// ones, which must all be there. Fingerprints may contain spaces. Without
// pinned fingerprints, it keeps every key: the key file itself is the pin,
// as for the gpgKey of a repository in a manifest.
func NewKeyring(data []byte, pinned ...string) (*Keyring, error) {
	entities, err := readEntities(data)
	if err != nil {
		return nil, fmt.Errorf("reading keys: %w", err)
	}
	if len(pinned) == 0 {
		return &Keyring{entities: entities}, nil
	}
	k := &Keyring{}
	for _, fp := range pinned {
		fp = strings.ToUpper(strings.ReplaceAll(fp, " ", ""))
		if !fingerprintRE.MatchString(fp) {
			return nil, fmt.Errorf("invalid fingerprint %q", fp)
		}
		i := slices.IndexFunc(entities, func(e *openpgp.Entity) bool { return fingerprint(e.PrimaryKey) == fp })
		if i < 0 {
			return nil, fmt.Errorf("the keys don't include the pinned key %s", fp)
		}
		k.entities = append(k.entities, entities[i])
	}
	return k, nil
}

// Fingerprints returns the fingerprints of the keyring's keys.
func (k *Keyring) Fingerprints() []string {
	fps := make([]string, len(k.entities))
	for i, e := range k.entities {
		fps[i] = fingerprint(e.PrimaryKey)
	}
	return fps
}

const clearsignHeader = "-----BEGIN PGP SIGNED MESSAGE-----"

// VerifyClearsigned checks a clearsigned message, such as an InRelease file,
// and returns the text it signs and the fingerprint of the key that signed
// it. Like apt, it rejects anything around the message but whitespace after
// it, since that wouldn't be signed.
func (k *Keyring) VerifyClearsigned(message []byte) (text []byte, signer string, err error) {
	if !bytes.HasPrefix(message, []byte(clearsignHeader)) {
		return nil, "", errors.New("not a clearsigned message: it doesn't start with " + clearsignHeader)
	}
	block, rest := clearsign.Decode(message)
	if block == nil {
		return nil, "", errors.New("malformed clearsigned message")
	}
	if len(bytes.TrimSpace(rest)) > 0 {
		return nil, "", errors.New("unsigned data after the clearsigned message")
	}
	sig, err := io.ReadAll(block.ArmoredSignature.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading the signature: %w", err)
	}
	if signer, err = k.verify(block.Bytes, sig); err != nil {
		return nil, "", err
	}
	return block.Plaintext, signer, nil
}

// VerifyDetached checks a detached signature of data, armored or binary,
// such as Release.gpg, and returns the fingerprint of the key that made it.
func (k *Keyring) VerifyDetached(data, signature []byte) (signer string, err error) {
	if bytes.HasPrefix(bytes.TrimSpace(signature), []byte("-----BEGIN PGP")) {
		block, err := armor.Decode(bytes.NewReader(signature))
		if err != nil {
			return "", fmt.Errorf("reading the signature: %w", err)
		}
		if signature, err = io.ReadAll(block.Body); err != nil {
			return "", fmt.Errorf("reading the signature: %w", err)
		}
	}
	return k.verify(data, signature)
}

// verify checks a signature with the keys that are valid now: apt rejects a
// signature by a key that has since expired or been revoked, even though the
// key was valid when it signed.
func (k *Keyring) verify(data, sig []byte) (string, error) {
	t := now()
	var valid openpgp.EntityList
	for _, e := range k.entities {
		if _, err := e.VerifyPrimaryKey(t, config()); err != nil {
			continue
		}
		v := *e
		v.Subkeys = slices.DeleteFunc(slices.Clone(e.Subkeys), func(s openpgp.Subkey) bool {
			_, err := s.Verify(t, config())
			return err != nil
		})
		valid = append(valid, &v)
	}
	_, signer, err := openpgp.VerifyDetachedSignature(valid, bytes.NewReader(data), bytes.NewReader(sig), config())
	if errors.Is(err, pgperrors.ErrUnknownIssuer) {
		return "", k.unknownIssuer(sig, t)
	}
	if err != nil {
		return "", fmt.Errorf("bad signature: %w", err)
	}
	return fingerprint(signer.PrimaryKey), nil
}

// unknownIssuer explains why none of the keys that made sig is valid: they
// aren't pinned, or they have expired or been revoked.
func (k *Keyring) unknownIssuer(sig []byte, t time.Time) error {
	var issuers []string
	packets := packet.NewReader(bytes.NewReader(sig))
	for {
		p, err := packets.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("bad signature: %w", err)
		}
		s, ok := p.(*packet.Signature)
		if !ok {
			continue
		}
		issuer := "with an unknown ID"
		switch {
		case len(s.IssuerFingerprint) > 0:
			issuer = strings.ToUpper(hex.EncodeToString(s.IssuerFingerprint))
		case s.IssuerKeyId != nil:
			issuer = fmt.Sprintf("%016X", *s.IssuerKeyId)
		}
		if s.IssuerKeyId != nil {
			if err := k.invalidKey(*s.IssuerKeyId, t); err != nil {
				return fmt.Errorf("signed by key %s, which %s", issuer, unusable(err))
			}
		}
		issuers = append(issuers, issuer)
	}
	switch len(issuers) {
	case 0:
		return errors.New("bad signature: no signature packet")
	case 1:
		return fmt.Errorf("signed by key %s, which isn't pinned", issuers[0])
	default:
		return fmt.Errorf("signed by keys %s, none of which is pinned", strings.Join(issuers, ", "))
	}
}

// invalidKey returns why the pinned key or subkey with this ID isn't valid
// at t, or nil if no pinned key has the ID.
func (k *Keyring) invalidKey(id uint64, t time.Time) error {
	for _, e := range k.entities {
		if e.PrimaryKey.KeyId == id {
			_, err := e.VerifyPrimaryKey(t, config())
			return err
		}
		for _, s := range e.Subkeys {
			if s.PublicKey.KeyId == id {
				if _, err := e.VerifyPrimaryKey(t, config()); err != nil {
					return err
				}
				_, err := s.Verify(t, config())
				return err
			}
		}
	}
	return nil
}
