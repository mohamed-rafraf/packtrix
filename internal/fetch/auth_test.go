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

package fetch

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/packtrix/packtrix/pkg/apis/packtrix"
)

func TestBasicAndTokenAuth(t *testing.T) {
	s, requests := server(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		user, password, basic := r.BasicAuth()
		switch {
		case basic && user == "packtrix" && password == "s3cret":
		case r.Header.Get("Authorization") == "Bearer t0ken":
		default:
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	dir := t.TempDir()
	// Secrets in files usually end with a newline.
	if err := os.WriteFile(filepath.Join(dir, "password"), []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACKTRIX_TEST_TOKEN", "t0ken")
	c := newClient(t, Options{})

	for _, a := range []*packtrix.Auth{
		{Username: "packtrix", Password: "file:password"},
		{Token: "env:PACKTRIX_TEST_TOKEN"},
	} {
		auth, err := ResolveAuth(a, dir)
		if err != nil {
			t.Fatal(err)
		}
		if data, err := c.Get(t.Context(), s.URL, auth); err != nil || string(data) != "ok" {
			t.Errorf("%+v: Get = %q, %v", a, data, err)
		}
	}

	requests.Store(0)
	if _, err := c.Get(t.Context(), s.URL, nil); err == nil || !strings.HasSuffix(err.Error(), ": 401 Unauthorized") {
		t.Errorf("without credentials: error = %v", err)
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("a 401 was tried %d times, want once", n)
	}
}

// clientCertificate writes a certificate authority and a client
// certificate it signed into dir.
func clientCertificate(t *testing.T, dir string) *x509.CertPool {
	t.Helper()
	newKey := func() *ecdsa.PrivateKey {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	caKey, key := newKey(), newKey()
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Packtrix test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "packtrix"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for name, block := range map[string]*pem.Block{
		"client.crt": {Type: "CERTIFICATE", Bytes: der},
		"client.key": {Type: "EC PRIVATE KEY", Bytes: keyDER},
	} {
		if err := os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(block), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return pool
}

func TestClientCertificate(t *testing.T) {
	dir := t.TempDir()
	var requests atomic.Int32
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	s.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			requests.Add(1)
		}
	}
	s.Config.ErrorLog = log.New(io.Discard, "", 0)
	s.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientCertificate(t, dir)}
	s.StartTLS()
	defer s.Close()
	caFile := filepath.Join(dir, "server-ca.pem")
	if err := os.WriteFile(caFile, certificatePEM(s), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newClient(t, Options{CAFile: caFile})

	auth, err := ResolveAuth(&packtrix.Auth{ClientCert: "client.crt", ClientKey: filepath.Join(dir, "client.key")}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := c.Get(t.Context(), s.URL, auth); err != nil || string(data) != "ok" {
		t.Errorf("with the client certificate: Get = %q, %v", data, err)
	}

	// Without it, the server refuses the handshake, and retrying won't help.
	requests.Store(0)
	if _, err := c.Get(t.Context(), s.URL, nil); err == nil || !strings.Contains(err.Error(), "certificate required") {
		t.Errorf("without the client certificate: error = %v", err)
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("the refused handshake was tried %d times, want once", n)
	}
}

func TestResolveAuthErrors(t *testing.T) {
	if auth, err := ResolveAuth(nil, ""); auth != nil || err != nil {
		t.Errorf("ResolveAuth(nil) = %v, %v", auth, err)
	}
	for _, tt := range []struct {
		auth packtrix.Auth
		want string
	}{
		{packtrix.Auth{Entitlement: "/etc/pki/entitlement"}, "entitlement: RHEL subscriptions aren't supported yet"},
		{packtrix.Auth{Username: "packtrix", Password: "env:PACKTRIX_TEST_UNSET"}, "password: environment variable PACKTRIX_TEST_UNSET isn't set"},
		{packtrix.Auth{Token: "env:PACKTRIX_TEST_UNSET"}, "token: environment variable PACKTRIX_TEST_UNSET isn't set"},
		{packtrix.Auth{ClientCert: "client.crt", ClientKey: "client.key"}, "client certificate: open "},
	} {
		_, err := ResolveAuth(&tt.auth, t.TempDir())
		if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
			t.Errorf("%+v: error = %v, want %q", tt.auth, err, tt.want)
		}
	}
}
