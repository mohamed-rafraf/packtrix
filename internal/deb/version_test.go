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
	"testing"
)

func mustVersion(t testing.TB, s string) Version {
	t.Helper()
	v, err := ParseVersion(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestParseVersion(t *testing.T) {
	valid := map[string]Version{
		"1.0":               {Upstream: "1.0"},
		"1.34.1-1.1":        {Upstream: "1.34.1", Revision: "1.1"},
		"1:2.38-3ubuntu1":   {Epoch: 1, Upstream: "2.38", Revision: "3ubuntu1"},
		"0:1.0":             {Upstream: "1.0"},
		"2.0~rc1+dfsg-1":    {Upstream: "2.0~rc1+dfsg", Revision: "1"},
		"1.2-3-4":           {Upstream: "1.2-3", Revision: "4"}, // upstream may contain '-'
		"1:2:3-4":           {Epoch: 1, Upstream: "2:3", Revision: "4"},
		"3.0.13-0ubuntu3.5": {Upstream: "3.0.13", Revision: "0ubuntu3.5"},
	}
	for in, want := range valid {
		got, err := ParseVersion(in)
		if err != nil || got != want {
			t.Errorf("ParseVersion(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}

	invalid := []string{"", ":1", "a:1", "-1:1", "1:", "1.0-", "-1", "1.0 2", "1.0_1", "1.0-a:b", "1.0-1_2", "99999999999:1",
		"v1.0", "0::", "1:~1"} // the upstream version must start with a digit
	for _, in := range invalid {
		if v, err := ParseVersion(in); err == nil {
			t.Errorf("ParseVersion(%q) = %+v, want error", in, v)
		}
	}
}

func TestVersionString(t *testing.T) {
	for in, want := range map[string]string{
		"1.0":             "1.0",
		"0:1.0":           "1.0",
		"1:2.38-3ubuntu1": "1:2.38-3ubuntu1",
		"0:1:2":           "0:1:2", // without the epoch, "1" would become one
	} {
		if got := mustVersion(t, in).String(); got != want {
			t.Errorf("ParseVersion(%q).String() = %q, want %q", in, got, want)
		}
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		// Equal versions, including dpkg's equivalences.
		{"1.0", "1.0", 0},
		{"0", "00", 0},
		{"1.01", "1.1", 0},
		{"0:1.0", "1.0", 0},
		{"1.0", "1.0-0", 0}, // no revision sorts like "0"
		{"1.2.3", "1.2.3", 0},

		// Numbers compare as numbers, of any length.
		{"1.9", "1.10", -1},
		{"9", "10", -1},
		{"2.30-0ubuntu1", "2.30-0ubuntu10", -1},
		{"1.99999999999999999999999", "1.100000000000000000000000", -1},

		// The epoch wins over everything else.
		{"1:1.0", "2.0", 1},
		{"1:0.1", "0:99", 1},

		// The revision decides when the upstream versions are equal.
		{"1.0-1", "1.0-2", -1},
		{"1.0-1ubuntu1", "1.0-1", 1},

		// deb-version(7): ~~ < ~~a < ~ < (the empty part) < a.
		{"1.0~~", "1.0~~a", -1},
		{"1.0~~a", "1.0~", -1},
		{"1.0~", "1.0", -1},
		{"1.0", "1.0a", -1},

		// Pre-releases sort before the release, and + suffixes after it.
		{"1.0~rc1", "1.0", -1},
		{"1.0~rc1", "1.0~rc2", -1},
		{"1.0", "1.0+b1", -1},
		{"2.0~beta1-1", "2.0-1", -1},

		// Letters sort before other characters.
		{"1.0a", "1.0+", -1},
		{"1.0a", "1.0.", -1},

		// Real versions from the Ubuntu and Kubernetes archives.
		{"3.0.13-0ubuntu3.4", "3.0.13-0ubuntu3.5", -1},
		{"1.34.1-1.1", "1.34.0-1.1", 1},
		{"2.39-0ubuntu8.3", "2.39-0ubuntu8", 1},
	}
	for _, tt := range tests {
		a, b := mustVersion(t, tt.a), mustVersion(t, tt.b)
		if got := Compare(a, b); got != tt.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
		if got := Compare(b, a); got != -tt.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tt.b, tt.a, got, -tt.want)
		}
	}
}

// Compare must be a total order on every valid version, and String must
// produce a version that parses back to an equal one.
func FuzzCompare(f *testing.F) {
	for _, s := range [][3]string{
		{"1.0", "1.0~rc1", "1:0.1"},
		{"1.0~~", "1.0~~a", "1.0~"},
		{"2.30-0ubuntu1", "2.30-0ubuntu10", "2.30"},
		{"0", "00", "000.1"},
		{"1.0+b1", "1.0a", "1.0."},
	} {
		f.Add(s[0], s[1], s[2])
	}
	f.Fuzz(func(t *testing.T, sa, sb, sc string) {
		a, errA := ParseVersion(sa)
		b, errB := ParseVersion(sb)
		c, errC := ParseVersion(sc)
		if errA != nil || errB != nil || errC != nil {
			return
		}
		if Compare(a, a) != 0 {
			t.Fatalf("Compare(%q, itself) != 0", sa)
		}
		if Compare(a, b) != -Compare(b, a) {
			t.Fatalf("Compare(%q, %q) = %d but Compare(%q, %q) = %d", sa, sb, Compare(a, b), sb, sa, Compare(b, a))
		}
		if Compare(a, b) <= 0 && Compare(b, c) <= 0 && Compare(a, c) > 0 {
			t.Fatalf("not transitive: %q <= %q <= %q but %q > %q", sa, sb, sc, sa, sc)
		}
		again, err := ParseVersion(a.String())
		if err != nil || Compare(a, again) != 0 {
			t.Fatalf("ParseVersion(%q.String() = %q) = %+v, %v", sa, a.String(), again, err)
		}
	})
}
