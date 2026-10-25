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

// Package fetch downloads from upstream repositories, the way corporate
// networks need it: through the proxy of the environment, trusting a private
// certificate authority, with each repository's credentials, retrying and
// resuming.
package fetch

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http/httpproxy"

	"github.com/packtrix/packtrix/internal/version"
)

// Options configure the client of a run.
type Options struct {
	// CAFile is a PEM file of certificate authorities to trust on top of
	// the system's, such as the one of a TLS-inspecting proxy.
	CAFile string
	// Retries is how many times a request is retried after a network error
	// or a 5xx or 429 response. Zero means 4; a negative value, none.
	Retries int
	// Backoff is the wait before the first retry, doubled before each next
	// one. Zero means one second.
	Backoff time.Duration
	// Concurrency is the most requests in flight at once. Zero means 8.
	Concurrency int
	// Progress, if set, is called as data arrives, with the bytes received
	// so far and the total, or -1 when the server doesn't say.
	Progress func(url string, done, total int64)
}

// Client downloads from upstream repositories. It is safe for concurrent
// use.
type Client struct {
	opts      Options
	base      *http.Transport
	slots     chan struct{}
	userAgent string

	mu      sync.Mutex
	clients map[*tls.Certificate]*http.Client
}

// New returns the client of a run. It reads the proxy settings from
// HTTP_PROXY, HTTPS_PROXY and NO_PROXY, and trusts the system's certificate
// authorities, those of SSL_CERT_FILE, and those of opts.CAFile.
func New(opts Options) (*Client, error) {
	if opts.Retries == 0 {
		opts.Retries = 4
	}
	if opts.Backoff == 0 {
		opts.Backoff = time.Second
	}
	if opts.Concurrency == 0 {
		opts.Concurrency = 8
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if opts.CAFile != "" {
		pem, err := os.ReadFile(opts.CAFile)
		if err != nil {
			return nil, fmt.Errorf("reading the CA file: %w", err)
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%s has no PEM certificate", opts.CAFile)
		}
	}
	proxy := httpproxy.FromEnvironment().ProxyFunc()
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return &Client{
		opts: opts,
		base: &http.Transport{
			Proxy:                 func(r *http.Request) (*url.URL, error) { return proxy(r.URL) },
			DialContext:           dialer.DialContext,
			TLSClientConfig:       &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout:   30 * time.Second,
			ResponseHeaderTimeout: time.Minute,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConnsPerHost:   opts.Concurrency,
			ForceAttemptHTTP2:     true,
		},
		slots:     make(chan struct{}, opts.Concurrency),
		userAgent: "packtrix/" + version.Get().Version,
		clients:   map[*tls.Certificate]*http.Client{},
	}, nil
}

// StatusError is a response that isn't a success.
type StatusError struct {
	// URL is the URL, without its password.
	URL    string
	Code   int
	Status string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("GET %s: %s", e.URL, e.Status)
}

// IsNotFound reports whether err is a 404 response.
func IsNotFound(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Code == http.StatusNotFound
}

// Get fetches a file into memory, such as an index.
func (c *Client) Get(ctx context.Context, rawURL string, auth *Auth) ([]byte, error) {
	var data []byte
	err := c.retry(ctx, func() error {
		resp, err := c.request(ctx, rawURL, auth, 0)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		data, err = io.ReadAll(c.progress(rawURL, resp.Body, 0, resp.ContentLength))
		return retryable(err, rawURL)
	})
	return data, err
}

// Download fetches a file into path. If path already holds the start of
// it, from an attempt that was interrupted, it fetches only the rest.
func (c *Client) Download(ctx context.Context, rawURL string, auth *Auth, path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	err = c.retry(ctx, func() error {
		offset, err := f.Seek(0, io.SeekEnd)
		if err != nil {
			return err
		}
		resp, err := c.request(ctx, rawURL, auth, offset)
		var se *StatusError
		if offset > 0 && errors.As(err, &se) && se.Code == http.StatusRequestedRangeNotSatisfiable {
			// What we have doesn't fit what the server has: start over.
			if err := restart(f); err != nil {
				return err
			}
			return temporary{err}
		}
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		total := resp.ContentLength
		switch {
		case resp.StatusCode == http.StatusPartialContent && contentRangeStart(resp) == offset:
			if total >= 0 {
				total += offset
			}
		case resp.StatusCode == http.StatusOK:
			// The server sends the whole file.
			if err := restart(f); err != nil {
				return err
			}
			offset = 0
		default:
			// The range doesn't fit what the server has: start over.
			if err := restart(f); err != nil {
				return err
			}
			return temporary{fmt.Errorf("GET %s: unexpected %s to a range request; starting over", redact(rawURL), resp.Status)}
		}
		_, err = io.Copy(f, c.progress(rawURL, resp.Body, offset, total))
		return retryable(err, rawURL)
	})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func restart(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err := f.Seek(0, io.SeekStart)
	return err
}

// contentRangeStart returns the first byte of a "bytes 100-199/200"
// Content-Range, or -1.
func contentRangeStart(resp *http.Response) int64 {
	cr, ok := strings.CutPrefix(resp.Header.Get("Content-Range"), "bytes ")
	if !ok {
		return -1
	}
	start, _, _ := strings.Cut(cr, "-")
	n, err := strconv.ParseInt(start, 10, 64)
	if err != nil {
		return -1
	}
	return n
}

// request sends a GET from byte offset on and returns a successful
// response. It holds one of the client's slots until the body is closed.
func (c *Client) request(ctx context.Context, rawURL string, auth *Auth, offset int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %s: %w", redact(rawURL), errors.Unwrap(err))
	}
	req.Header.Set("User-Agent", c.userAgent)
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	auth.apply(req)

	select {
	case c.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	resp, err := c.client(auth).Do(req)
	if err != nil {
		<-c.slots
		return nil, c.requestError(err, req.URL)
	}
	resp.Body = &releasingBody{ReadCloser: resp.Body, release: func() { <-c.slots }}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_ = resp.Body.Close()
		err := &StatusError{URL: redact(rawURL), Code: resp.StatusCode, Status: resp.Status}
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return nil, temporary{err}
		}
		return nil, err
	}
	return resp, nil
}

// requestError explains why a request got no response.
func (c *Client) requestError(err error, u *url.URL) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	var unknownAuthority x509.UnknownAuthorityError
	var verification *tls.CertificateVerificationError
	var op *net.OpError
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.As(err, &unknownAuthority), errors.As(err, &verification):
		return fmt.Errorf("%s: the server's certificate isn't trusted (%w); if a proxy or mirror uses a private certificate authority, pass its certificate with --ca-file", u.Host, err)
	case errors.As(err, &op) && op.Op == "remote error":
		// The server sent a TLS alert, such as for a missing client
		// certificate: trying again won't help.
		return fmt.Errorf("GET %s: %w", u.Redacted(), err)
	}
	return temporary{fmt.Errorf("GET %s: %w", u.Redacted(), err)}
}

// client returns the HTTP client for a repository's credentials: one per
// client certificate, since TLS connections are pooled. It follows
// redirects, and drops the credentials when a redirect leaves the host.
func (c *Client) client(auth *Auth) *http.Client {
	var cert *tls.Certificate
	if auth != nil {
		cert = auth.Certificate
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	hc, ok := c.clients[cert]
	if !ok {
		t := c.base
		if cert != nil {
			t = c.base.Clone()
			t.TLSClientConfig.Certificates = []tls.Certificate{*cert}
		}
		hc = &http.Client{Transport: t}
		c.clients[cert] = hc
	}
	return hc
}

type releasingBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *releasingBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.release)
	return err
}

// progress reports what is read from r, from offset on.
func (c *Client) progress(rawURL string, r io.Reader, offset, total int64) io.Reader {
	if c.opts.Progress == nil {
		return r
	}
	return &progressReader{r: r, done: offset, total: total, report: func(done, total int64) {
		c.opts.Progress(redact(rawURL), done, total)
	}}
}

type progressReader struct {
	r           io.Reader
	done, total int64
	report      func(done, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.done += int64(n)
		p.report(p.done, p.total)
	}
	return n, err
}

// temporary marks an error worth retrying.
type temporary struct{ error }

// retryable marks an error reading a response body as worth retrying: the
// connection broke.
func retryable(err error, rawURL string) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return temporary{fmt.Errorf("GET %s: %w", redact(rawURL), err)}
}

// retry runs attempt until it succeeds, fails for good, or runs out of
// retries, waiting longer before each retry.
func (c *Client) retry(ctx context.Context, attempt func() error) error {
	wait := c.opts.Backoff
	for i := 0; ; i++ {
		err := attempt()
		var t temporary
		if err == nil || !errors.As(err, &t) || i >= c.opts.Retries {
			if errors.As(err, &t) {
				return t.error
			}
			return err
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
		wait *= 2
	}
}

// redact removes the password from a URL, for messages.
func redact(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "(invalid URL)"
	}
	return u.Redacted()
}
