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
	"bytes"
	"context"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newClient(t *testing.T, opts Options) *Client {
	t.Helper()
	if opts.Backoff == 0 {
		opts.Backoff = time.Millisecond
	}
	c, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// server counts its requests and answers them with handle.
func server(t *testing.T, handle func(w http.ResponseWriter, r *http.Request, n int)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handle(w, r, int(requests.Add(1)))
	}))
	t.Cleanup(s.Close)
	return s, &requests
}

// breakAfter sends the headers for the whole body but only its first n
// bytes, then drops the connection.
func breakAfter(t *testing.T, w http.ResponseWriter, body []byte, n int) {
	t.Helper()
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body[:n])
	w.(http.Flusher).Flush()
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		t.Error(err)
		return
	}
	_ = conn.Close()
}

func TestGet(t *testing.T) {
	s, _ := server(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		if !strings.HasPrefix(r.UserAgent(), "packtrix/") {
			t.Errorf("User-Agent = %q", r.UserAgent())
		}
		_, _ = w.Write([]byte("Origin: Ubuntu\n"))
	})
	var done, total int64
	c := newClient(t, Options{Progress: func(_ string, d, tot int64) { done, total = d, tot }})
	data, err := c.Get(t.Context(), s.URL+"/InRelease", nil)
	if err != nil || string(data) != "Origin: Ubuntu\n" {
		t.Fatalf("Get = %q, %v", data, err)
	}
	if done != 15 || total != 15 {
		t.Errorf("progress = %d/%d, want 15/15", done, total)
	}

	if _, err := c.Get(t.Context(), "http://[::1", nil); err == nil || !strings.HasPrefix(err.Error(), "invalid URL ") {
		t.Errorf("invalid URL: error = %v", err)
	}
}

func TestRetries(t *testing.T) {
	for _, tt := range []struct {
		name     string
		handle   func(w http.ResponseWriter, r *http.Request, n int)
		requests int32
		want     string
	}{
		{"5xx, then success", func(w http.ResponseWriter, _ *http.Request, n int) {
			if n <= 2 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte("ok"))
		}, 3, ""},
		{"429, then success", func(w http.ResponseWriter, _ *http.Request, n int) {
			if n == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_, _ = w.Write([]byte("ok"))
		}, 2, ""},
		{"connection dropped, then success", func(w http.ResponseWriter, _ *http.Request, n int) {
			if n == 1 {
				breakAfter(t, w, []byte("ok"), 1)
				return
			}
			_, _ = w.Write([]byte("ok"))
		}, 2, ""},
		{"404 isn't retried", func(w http.ResponseWriter, _ *http.Request, _ int) {
			w.WriteHeader(http.StatusNotFound)
		}, 1, ": 404 Not Found"},
		{"retries run out", func(w http.ResponseWriter, _ *http.Request, _ int) {
			w.WriteHeader(http.StatusInternalServerError)
		}, 5, ": 500 Internal Server Error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, requests := server(t, tt.handle)
			data, err := newClient(t, Options{}).Get(t.Context(), s.URL+"/file", nil)
			switch {
			case tt.want == "" && (err != nil || string(data) != "ok"):
				t.Errorf("Get = %q, %v, want ok", data, err)
			case tt.want != "" && (err == nil || err.Error() != "GET "+s.URL+"/file"+tt.want):
				t.Errorf("error = %v, want %q", err, tt.want)
			}
			if got := requests.Load(); got != tt.requests {
				t.Errorf("%d requests, want %d", got, tt.requests)
			}
		})
	}
}

func TestIsNotFound(t *testing.T) {
	s, _ := server(t, func(w http.ResponseWriter, _ *http.Request, _ int) { w.WriteHeader(http.StatusNotFound) })
	_, err := newClient(t, Options{}).Get(t.Context(), s.URL+"/by-hash/SHA256/abc", nil)
	if !IsNotFound(err) || IsNotFound(errors.New("other")) {
		t.Errorf("IsNotFound(%v) = false", err)
	}
}

func TestDownloadResumes(t *testing.T) {
	content := make([]byte, 100_000)
	_, _ = rand.Read(content)
	serve := func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(content))
	}

	for _, tt := range []struct {
		name    string
		partial []byte
		handle  func(w http.ResponseWriter, r *http.Request, n int)
		// ranges are the Range headers of the requests.
		ranges []string
	}{
		{"interrupted", nil, func(w http.ResponseWriter, r *http.Request, n int) {
			if n == 1 {
				breakAfter(t, w, content, 40_000)
				return
			}
			serve(w, r)
		}, []string{"", "bytes=40000-"}},
		{"left by an earlier run", content[:30_000], func(w http.ResponseWriter, r *http.Request, _ int) {
			serve(w, r)
		}, []string{"bytes=30000-"}},
		{"server ignores the range", []byte("junk"), func(w http.ResponseWriter, _ *http.Request, _ int) {
			_, _ = w.Write(content)
		}, []string{"bytes=4-"}},
		{"longer than the file", append(bytes.Clone(content), "junk"...), func(w http.ResponseWriter, r *http.Request, _ int) {
			serve(w, r)
		}, []string{"bytes=100004-", ""}},
		{"range that doesn't start at the offset", content[:10], func(w http.ResponseWriter, r *http.Request, n int) {
			if n == 1 {
				w.Header().Set("Content-Range", "bytes 0-99999/100000")
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(content)
				return
			}
			serve(w, r)
		}, []string{"bytes=10-", ""}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var ranges []string
			s, _ := server(t, func(w http.ResponseWriter, r *http.Request, n int) {
				mu.Lock()
				ranges = append(ranges, r.Header.Get("Range"))
				mu.Unlock()
				tt.handle(w, r, n)
			})
			path := filepath.Join(t.TempDir(), "package.deb")
			if tt.partial != nil {
				if err := os.WriteFile(path, tt.partial, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var last int64
			c := newClient(t, Options{Progress: func(_ string, done, _ int64) { last = done }})
			if err := c.Download(t.Context(), s.URL+"/package.deb", nil, path); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, content) {
				t.Errorf("downloaded %d bytes that differ from the %d of the file", len(got), len(content))
			}
			if strings.Join(ranges, ",") != strings.Join(tt.ranges, ",") {
				t.Errorf("Range headers = %q, want %q", ranges, tt.ranges)
			}
			if last != int64(len(content)) {
				t.Errorf("progress ended at %d, want %d", last, len(content))
			}
		})
	}

	// A file that can't be created fails without a request.
	if err := newClient(t, Options{}).Download(t.Context(), "http://127.0.0.1:1/x", nil, filepath.Join(t.TempDir(), "missing", "x")); err == nil {
		t.Error("Download into a missing directory succeeded")
	}
}

func TestConcurrency(t *testing.T) {
	var inFlight, most atomic.Int32
	s, _ := server(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		n := inFlight.Add(1)
		for {
			m := most.Load()
			if n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
		_, _ = w.Write([]byte("ok"))
	})
	c := newClient(t, Options{Concurrency: 2})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := c.Get(t.Context(), s.URL, nil); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if got := most.Load(); got != 2 {
		t.Errorf("at most %d requests in flight, want 2", got)
	}
}

func TestCancel(t *testing.T) {
	s, _ := server(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := newClient(t, Options{Backoff: time.Hour}).Get(ctx, s.URL, nil)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 10*time.Second {
		t.Errorf("error = %v after %s, want the context's error right away", err, time.Since(start))
	}
}

func TestProxy(t *testing.T) {
	var proxied []string
	proxy, _ := server(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		proxied = append(proxied, r.URL.String())
		_, _ = w.Write([]byte("through the proxy"))
	})
	for _, name := range []string{"HTTP_PROXY", "http_proxy"} {
		t.Setenv(name, proxy.URL)
	}
	for _, name := range []string{"NO_PROXY", "no_proxy"} {
		t.Setenv(name, "internal.invalid")
	}
	c := newClient(t, Options{Retries: -1})

	data, err := c.Get(t.Context(), "http://archive.ubuntu.invalid/ubuntu/dists/noble/InRelease", nil)
	if err != nil || string(data) != "through the proxy" {
		t.Fatalf("Get = %q, %v", data, err)
	}
	// NO_PROXY hosts are reached directly, which fails for .invalid.
	if _, err := c.Get(t.Context(), "http://internal.invalid/repo/InRelease", nil); err == nil {
		t.Error("a NO_PROXY host was proxied")
	}
	if want := []string{"http://archive.ubuntu.invalid/ubuntu/dists/noble/InRelease"}; strings.Join(proxied, ",") != strings.Join(want, ",") {
		t.Errorf("the proxy got %q, want %q", proxied, want)
	}
}

func certificatePEM(s *httptest.Server) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw})
}

func TestCustomCA(t *testing.T) {
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	s.Config.ErrorLog = log.New(io.Discard, "", 0)
	s.StartTLS()
	defer s.Close()
	host := strings.TrimPrefix(s.URL, "https://")

	_, err := newClient(t, Options{}).Get(t.Context(), s.URL, nil)
	if err == nil || !strings.HasPrefix(err.Error(), host+": the server's certificate isn't trusted") || !strings.Contains(err.Error(), "pass its certificate with --ca-file") {
		t.Errorf("error = %v, want one naming %s and suggesting --ca-file", err, host)
	}

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, certificatePEM(s), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := newClient(t, Options{CAFile: caFile}).Get(t.Context(), s.URL, nil); err != nil || string(data) != "ok" {
		t.Errorf("with the CA: Get = %q, %v", data, err)
	}

	notPEM := filepath.Join(t.TempDir(), "ca.der")
	if err := os.WriteFile(notPEM, s.Certificate().Raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]string{
		notPEM:                                notPEM + " has no PEM certificate",
		filepath.Join(t.TempDir(), "missing"): "reading the CA file: open ",
	} {
		if _, err := New(Options{CAFile: file}); err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("New(%s) = %v, want %q", file, err, want)
		}
	}
}

func TestPasswordNeverPrinted(t *testing.T) {
	s, _ := server(t, func(w http.ResponseWriter, _ *http.Request, _ int) { w.WriteHeader(http.StatusNotFound) })
	var progressURL string
	c := newClient(t, Options{Retries: -1, Progress: func(u string, _, _ int64) { progressURL = u }})
	withPassword := strings.Replace(s.URL, "http://", "http://user:s3cret@", 1)
	for _, u := range []string{withPassword + "/missing", "http://user:s3cret@127.0.0.1:1/refused"} {
		_, err := c.Get(t.Context(), u, nil)
		if err == nil || strings.Contains(err.Error(), "s3cret") || !strings.Contains(err.Error(), "user:xxxxx@") {
			t.Errorf("error = %v, want the URL without its password", err)
		}
	}
	if strings.Contains(progressURL, "s3cret") {
		t.Errorf("progress got %q", progressURL)
	}
}

func TestLiveGet(t *testing.T) {
	if testing.Short() {
		t.Skip("downloads from archive.ubuntu.com")
	}
	data, err := newClient(t, Options{}).Get(t.Context(), "https://archive.ubuntu.com/ubuntu/dists/noble/InRelease", nil)
	if err != nil || !bytes.HasPrefix(data, []byte("-----BEGIN PGP SIGNED MESSAGE-----")) {
		t.Errorf("Get = %.40q, %v", data, err)
	}
}
