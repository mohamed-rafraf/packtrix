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
	"strings"
	"testing"
)

func TestConstraintMatches(t *testing.T) {
	tests := []struct {
		constraint string
		version    string
		want       bool
	}{
		// No constraint: the newest version is picked elsewhere.
		{"", "1.0", true},

		// An upstream version, any revision and any epoch.
		{"1.34.1", "1.34.1-1.1", true},
		{"1.34.1", "1.34.1", true},
		{"1.34.1", "1:1.34.1-2", true},
		{"1.34.1", "1.34.01-1", true}, // equal by dpkg's rules
		{"1.34.1", "1.34.10-1", false},
		{"1.34.1", "1.34.1~rc1-1", false},

		// A wildcard: 1.34 and every 1.34.x.
		{"1.34.*", "1.34.0-1.1", true},
		{"1.34.*", "1.34.5-1.1", true},
		{"1.34.*", "1.34-1", true},
		{"1.34.*", "1.340.0-1", false},
		{"1.34.*", "1.35.0-1.1", false},
		{"1.34.*", "1.3.4-1", false},

		// An exact version.
		{"1.34.1-1.1", "1.34.1-1.1", true},
		{"1.34.1-1.1", "1.34.1-1.2", false},
		{"1.34.1-1.1", "1:1.34.1-1.1", true},

		// An epoch must match.
		{"1:2.0", "1:2.0-3", true},
		{"1:2.0", "2.0-3", false},
		{"0:2.0", "2.0-3", true},
		{"1:2.*", "1:2.5-1", true},
		{"1:2.*", "2:2.5-1", false},
	}
	for _, tt := range tests {
		c, err := ParseConstraint(tt.constraint)
		if err != nil {
			t.Fatal(err)
		}
		if got := c.Matches(mustVersion(t, tt.version)); got != tt.want {
			t.Errorf("%q matches %q = %v, want %v", tt.constraint, tt.version, got, tt.want)
		}
	}
}

func TestParseConstraintErrors(t *testing.T) {
	for in, want := range map[string]string{
		"*":        "only allowed at the end",
		"1.*.3":    "only allowed at the end",
		"1.34*":    "only allowed at the end",
		".*":       "empty upstream version",
		"1.34.*-1": "only allowed at the end",
		"1.34-1.*": "can't be combined with a revision",
		"1.34 1":   "isn't allowed",
		"x:1.0":    "epoch",
	} {
		_, err := ParseConstraint(in)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseConstraint(%q) error = %v, want it to contain %q", in, err, want)
		}
	}
}

func TestConstraintString(t *testing.T) {
	for _, s := range []string{"", "1.34.*", "1:2.0-1"} {
		c, err := ParseConstraint(s)
		if err != nil || c.String() != s {
			t.Errorf("ParseConstraint(%q).String() = %q, %v", s, c.String(), err)
		}
	}
}
