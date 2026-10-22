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

package v1alpha1

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestByteSizeJSON(t *testing.T) {
	var b ByteSize
	if err := json.Unmarshal([]byte(`"4GiB"`), &b); err != nil || b != 4<<30 {
		t.Errorf("unmarshal \"4GiB\" = %d, %v; want %d", b, err, 4<<30)
	}
	if err := json.Unmarshal([]byte(`4096`), &b); err != nil || b != 4096 {
		t.Errorf("unmarshal number = %d, %v; want 4096", b, err)
	}
	if err := json.Unmarshal([]byte(`-1`), &b); err == nil {
		t.Error("unmarshal -1 succeeded, want error")
	}
	out, err := json.Marshal(ByteSize(4 << 30))
	if err != nil || string(out) != `"4GiB"` {
		t.Errorf("marshal = %s, %v; want \"4GiB\"", out, err)
	}
}

func TestDurationJSON(t *testing.T) {
	var d Duration
	if err := json.Unmarshal([]byte(`"72h"`), &d); err != nil || time.Duration(d) != 72*time.Hour {
		t.Errorf("unmarshal = %v, %v; want 72h", time.Duration(d), err)
	}
	if err := json.Unmarshal([]byte(`72`), &d); err == nil || !strings.Contains(err.Error(), "must be a string") {
		t.Errorf("unmarshal number error = %v, want a hint to use a string", err)
	}
	out, err := json.Marshal(d)
	if err != nil || string(out) != `"72h0m0s"` {
		t.Errorf("marshal = %s, %v", out, err)
	}
}
