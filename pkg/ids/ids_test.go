package ids_test

import (
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/andytrue7/coinly/pkg/ids"
)

func TestNew_IsVersion7(t *testing.T) {
	id := ids.New()

	parsed, err := uuid.Parse(id.String())
	if err != nil {
		t.Fatalf("New() = %q: not a valid UUID: %v", id, err)
	}
	if got := parsed.Version(); got != 7 {
		t.Errorf("New() version = %d, want 7", got)
	}
	if got := parsed.Variant(); got != uuid.RFC4122 {
		t.Errorf("New() variant = %v, want RFC4122", got)
	}
}

func TestNew_Unique(t *testing.T) {
	const n = 10_000
	seen := make(map[ids.ID]struct{}, n)
	for range n {
		id := ids.New()
		if _, dup := seen[id]; dup {
			t.Fatalf("New() produced duplicate %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestNew_SortsByCreationTime(t *testing.T) {
	// IDs generated across distinct milliseconds must sort lexically in
	// creation order — the whole point of v7 over v4 (ADR 0003).
	const n = 5
	generated := make([]ids.ID, 0, n)
	for range n {
		generated = append(generated, ids.New())
		time.Sleep(2 * time.Millisecond)
	}

	sorted := make([]ids.ID, len(generated))
	copy(sorted, generated)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].String() < sorted[j].String() })

	for i := range generated {
		if generated[i] != sorted[i] {
			t.Fatalf("IDs not in creation order:\n got %v\nwant %v", generated, sorted)
		}
	}
}

func TestParse(t *testing.T) {
	want := ids.New()

	tests := []struct {
		name    string
		in      string
		want    ids.ID
		wantErr bool
	}{
		{name: "round-trip", in: want.String(), want: want},
		{name: "uppercase accepted", in: toUpper(want.String()), want: want},
		{name: "empty", in: "", wantErr: true},
		{name: "garbage", in: "not-a-uuid", wantErr: true},
		{name: "wrong length", in: want.String()[:35], wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ids.Parse(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) = %v, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("Parse(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestID_IsZero(t *testing.T) {
	var zero ids.ID
	if !zero.IsZero() {
		t.Error("zero ID: IsZero() = false, want true")
	}
	if ids.New().IsZero() {
		t.Error("New(): IsZero() = true, want false")
	}
}

func TestID_String_Canonical(t *testing.T) {
	s := ids.New().String()
	if len(s) != 36 {
		t.Fatalf("String() len = %d, want 36 (canonical hyphenated form): %q", len(s), s)
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				t.Errorf("String()[%d] = %q, want '-'", i, r)
			}
		default:
			isHex := r >= '0' && r <= '9' || r >= 'a' && r <= 'f'
			if !isHex {
				t.Errorf("String()[%d] = %q, want lowercase hex", i, r)
			}
		}
	}
}

func toUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}
