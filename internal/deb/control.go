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
	"bufio"
	"fmt"
	"io"
	"strings"
)

// maxLine bounds the length of one line. The longest lines of real indexes
// are Provides fields of a few hundred kilobytes.
const maxLine = 16 << 20

// Paragraph is one paragraph of a deb822 file, such as one package of a
// Packages index.
type Paragraph struct {
	fields map[string]string
}

// Get returns the value of a field, or "" when the paragraph doesn't have it.
// Field names are case-insensitive. The lines of a multiline value are joined
// with "\n", without the space that starts each continuation line.
func (p Paragraph) Get(name string) string {
	return p.fields[strings.ToLower(name)]
}

// ReadParagraphs calls fn with each paragraph of a deb822 file, in order. It
// stops at the first error, from the file or from fn.
func ReadParagraphs(r io.Reader, fn func(Paragraph) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(nil, maxLine)
	var (
		p    Paragraph
		last string
		line int
	)
	flush := func() error {
		if p.fields == nil {
			return nil
		}
		err := fn(p)
		p, last = Paragraph{}, ""
		return err
	}
	for sc.Scan() {
		line++
		text := sc.Text()
		switch {
		case strings.TrimSpace(text) == "":
			if err := flush(); err != nil {
				return err
			}
		case strings.HasPrefix(text, "#"):
		case text[0] == ' ' || text[0] == '\t':
			if last == "" {
				return fmt.Errorf("line %d: continuation line outside a field", line)
			}
			p.fields[last] += "\n" + text[1:]
		default:
			name, value, ok := strings.Cut(text, ":")
			if !ok || name == "" || strings.ContainsAny(name, " \t") {
				return fmt.Errorf("line %d: %q is not a field", line, text)
			}
			last = strings.ToLower(name)
			if p.fields == nil {
				p.fields = map[string]string{}
			}
			if _, dup := p.fields[last]; dup {
				return fmt.Errorf("line %d: field %s appears twice in the paragraph", line, name)
			}
			p.fields[last] = strings.TrimSpace(value)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("line %d: %w", line+1, err)
	}
	return flush()
}
