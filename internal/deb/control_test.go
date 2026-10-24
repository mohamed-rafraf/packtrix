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

package deb

import (
	"errors"
	"strings"
	"testing"
	"testing/iotest"
)

func readAll(t *testing.T, text string) []Paragraph {
	t.Helper()
	var ps []Paragraph
	err := ReadParagraphs(strings.NewReader(text), func(p Paragraph) error {
		ps = append(ps, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func TestReadParagraphs(t *testing.T) {
	ps := readAll(t, `

Package: curl
Version: 8.5.0-2ubuntu10.6
Depends: libc6 (>= 2.34),
 libcurl4t64 (= 8.5.0-2ubuntu10.6)
Description: command line tool for transferring data with URL syntax
 curl is a command line tool for transferring data with URL syntax.
 .
 It supports many protocols.

# A comment, which some deb822 files allow.
package: libcurl4t64
VERSION:8.5.0-2ubuntu10.6
`)
	if len(ps) != 2 {
		t.Fatalf("got %d paragraphs, want 2", len(ps))
	}
	for _, tt := range []struct {
		p           int
		field, want string
	}{
		{0, "Package", "curl"},
		{0, "package", "curl"},
		{0, "Depends", "libc6 (>= 2.34),\nlibcurl4t64 (= 8.5.0-2ubuntu10.6)"},
		{0, "Description", "command line tool for transferring data with URL syntax\ncurl is a command line tool for transferring data with URL syntax.\n.\nIt supports many protocols."},
		{0, "Recommends", ""},
		{1, "Package", "libcurl4t64"},
		{1, "Version", "8.5.0-2ubuntu10.6"},
	} {
		if got := ps[tt.p].Get(tt.field); got != tt.want {
			t.Errorf("paragraph %d: Get(%q) = %q, want %q", tt.p, tt.field, got, tt.want)
		}
	}
}

func TestReadParagraphsEmpty(t *testing.T) {
	if ps := readAll(t, "\n \n"); len(ps) != 0 {
		t.Errorf("got %d paragraphs from blank lines, want none", len(ps))
	}
}

func TestReadParagraphsErrors(t *testing.T) {
	for _, tt := range []struct{ text, want string }{
		{" continued\n", "line 1: continuation line outside a field"},
		{"Package: curl\n\n continued\n", "line 3: continuation line outside a field"},
		{"Package: curl\nnot a field\n", `line 2: "not a field" is not a field`},
		{": empty name\n", `line 1: ": empty name" is not a field`},
		{"Package: curl\nPackage: wget\n", "line 2: field Package appears twice in the paragraph"},
		{"Package: curl\n" + strings.Repeat("x", maxLine+1), "line 2: bufio.Scanner: token too long"},
	} {
		err := ReadParagraphs(strings.NewReader(tt.text), func(Paragraph) error { return nil })
		if err == nil || err.Error() != tt.want {
			t.Errorf("ReadParagraphs(%.30q) = %v, want %q", tt.text, err, tt.want)
		}
	}
}

func TestReadParagraphsStops(t *testing.T) {
	stop := errors.New("stop")
	calls := 0
	err := ReadParagraphs(strings.NewReader("Package: aa\n\nPackage: bb\n"), func(Paragraph) error {
		calls++
		return stop
	})
	if !errors.Is(err, stop) || calls != 1 {
		t.Errorf("got %v after %d calls, want fn's error after the first paragraph", err, calls)
	}

	broken := iotest.ErrReader(errors.New("connection reset"))
	if err := ReadParagraphs(broken, func(Paragraph) error { return nil }); err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("got %v, want the reader's error", err)
	}
}
