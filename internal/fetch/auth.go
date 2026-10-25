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
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/packtrix/packtrix/internal/secret"
	"github.com/packtrix/packtrix/pkg/apis/packtrix"
)

// Auth holds the credentials of a repository, ready to use.
type Auth struct {
	// Username and Password are sent with basic authentication.
	Username string
	Password secret.Value
	// Token is sent as a bearer token.
	Token secret.Value
	// Certificate is a TLS client certificate.
	Certificate *tls.Certificate
}

// ResolveAuth reads the secrets and the client certificate that a
// manifest's auth references. Relative paths start from dir, the manifest's
// directory. A nil auth gives nil.
func ResolveAuth(a *packtrix.Auth, dir string) (*Auth, error) {
	if a == nil {
		return nil, nil
	}
	if a.Entitlement != "" {
		return nil, errors.New("entitlement: RHEL subscriptions aren't supported yet")
	}
	out := &Auth{Username: a.Username}
	var err error
	if a.Password != "" {
		if out.Password, err = secret.Resolve(a.Password, dir); err != nil {
			return nil, fmt.Errorf("password: %w", err)
		}
	}
	if a.Token != "" {
		if out.Token, err = secret.Resolve(a.Token, dir); err != nil {
			return nil, fmt.Errorf("token: %w", err)
		}
	}
	if a.ClientCert != "" {
		cert, err := tls.LoadX509KeyPair(relative(dir, a.ClientCert), relative(dir, a.ClientKey))
		if err != nil {
			return nil, fmt.Errorf("client certificate: %w", err)
		}
		out.Certificate = &cert
	}
	return out, nil
}

func relative(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

// apply adds the credentials to a request. A secret read from a file
// usually ends with a newline, which isn't part of it.
func (a *Auth) apply(req *http.Request) {
	if a == nil {
		return
	}
	value := func(v secret.Value) string { return strings.TrimRight(string(v.Bytes()), "\r\n") }
	switch {
	case a.Token.Bytes() != nil:
		req.Header.Set("Authorization", "Bearer "+value(a.Token))
	case a.Username != "":
		req.SetBasicAuth(a.Username, value(a.Password))
	}
}
