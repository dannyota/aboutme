// entry_test.go is hand-written, alongside section.go/section_test.go (see
// section.go's header) — not generated, not touched by generate.mjs.
//
// Covers design spec §3's draft-permissive contract at the entry-field
// level: every domain field is optional (only `id` is required — see
// entryBase in resume.schema.json), and a missing key ("never entered")
// must stay distinguishable from an explicit "" ("explicitly cleared")
// through a JSON round trip. quicktype's answer to "pointer or
// omitempty-with-explicit-null" (see the coordinator's fix-round-2 ask) is
// pointers with `omitempty`: every optional field is `*T` with a
// `,omitempty` json tag once resume.schema.json stopped requiring it, which
// is exactly what's needed — encoding/json's `omitempty` omits the key only
// for a nil pointer, and still emits `"field":""` for a non-nil pointer to
// an empty string.
package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func ptr[T any](v T) *T {
	return &v
}

func TestWorkEntry_AbsentFieldStaysAbsentThroughRoundTrip(t *testing.T) {
	const original = `{"id":"e1"}`

	var entry WorkEntry
	if err := json.Unmarshal([]byte(original), &entry); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if entry.JobTitle != nil {
		t.Fatalf("JobTitle: got %v, want nil (absent key)", entry.JobTitle)
	}
	if entry.Employer != nil || entry.EmployerLink != nil || entry.City != nil ||
		entry.Country != nil || entry.Dates != nil || entry.Description != nil || entry.IsHidden != nil {
		t.Fatalf("expected every optional field nil for a bare {\"id\":\"e1\"}, got %+v", entry)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), "jobTitle") {
		t.Errorf("re-marshaled JSON must omit an absent field's key entirely, got: %s", data)
	}

	var reparsed map[string]any
	if err := json.Unmarshal(data, &reparsed); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	if len(reparsed) != 1 || reparsed["id"] != "e1" {
		t.Fatalf("expected the round trip to keep exactly {\"id\":\"e1\"}, got: %s", data)
	}
}

func TestWorkEntry_ExplicitEmptyStringStaysPresentThroughRoundTrip(t *testing.T) {
	const original = `{"id":"e1","jobTitle":""}`

	var entry WorkEntry
	if err := json.Unmarshal([]byte(original), &entry); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if entry.JobTitle == nil {
		t.Fatalf("JobTitle: got nil, want a non-nil pointer to \"\" (explicitly cleared, not absent)")
	}
	if *entry.JobTitle != "" {
		t.Fatalf("JobTitle: got %q, want \"\"", *entry.JobTitle)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var reparsed map[string]any
	if err := json.Unmarshal(data, &reparsed); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	jobTitle, ok := reparsed["jobTitle"]
	if !ok {
		t.Fatalf("re-marshaled JSON must keep an explicitly-empty field's key present, got: %s", data)
	}
	if jobTitle != "" {
		t.Errorf("jobTitle: got %v, want \"\"", jobTitle)
	}
}

// TestWorkEntry_AbsentAndEmptyAreDistinguishableInGo is the direct Go-side
// assertion behind the two round-trip tests above: nil and a pointer to ""
// are different values, and only encoding/json's pointer+omitempty
// combination (as opposed to a plain string field, or omitempty on a plain
// string, which can't represent "absent" at all) can carry that difference
// across a decode.
func TestWorkEntry_AbsentAndEmptyAreDistinguishableInGo(t *testing.T) {
	var absent WorkEntry
	if err := json.Unmarshal([]byte(`{"id":"e1"}`), &absent); err != nil {
		t.Fatalf("Unmarshal (absent): %v", err)
	}

	var empty WorkEntry
	if err := json.Unmarshal([]byte(`{"id":"e1","jobTitle":""}`), &empty); err != nil {
		t.Fatalf("Unmarshal (empty): %v", err)
	}

	if absent.JobTitle == empty.JobTitle {
		t.Fatalf("expected different pointer identity/nilness for absent vs. explicit-empty, got JobTitle %v == %v", absent.JobTitle, empty.JobTitle)
	}
	if absent.JobTitle != nil {
		t.Errorf("absent.JobTitle: got %v, want nil", absent.JobTitle)
	}
	if empty.JobTitle == nil || *empty.JobTitle != "" {
		t.Errorf("empty.JobTitle: got %v, want a pointer to \"\"", empty.JobTitle)
	}
}

// TestDraftPartialEntry_OnlyIdAndJobTitleRoundTrips mirrors the exact
// example from design spec §3's draft-permissive rule and
// fixtures/draft-partial.json: a work entry with only id and jobTitle typed
// so far. Every other field must decode absent (nil), not zero-valued.
func TestDraftPartialEntry_OnlyIdAndJobTitleRoundTrips(t *testing.T) {
	const original = `{"id":"e1","jobTitle":"Engineer"}`

	var entry WorkEntry
	if err := json.Unmarshal([]byte(original), &entry); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if entry.ID != "e1" {
		t.Errorf("ID: got %q, want \"e1\"", entry.ID)
	}
	if entry.JobTitle == nil || *entry.JobTitle != "Engineer" {
		t.Errorf("JobTitle: got %v, want a pointer to \"Engineer\"", entry.JobTitle)
	}
	if entry.Employer != nil {
		t.Errorf("Employer: got %v, want nil (never typed)", entry.Employer)
	}
	if entry.IsHidden != nil {
		t.Errorf("IsHidden: got %v, want nil (never typed — isHidden is optional too, not just domain fields)", entry.IsHidden)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var reparsed map[string]any
	if err := json.Unmarshal(data, &reparsed); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	if len(reparsed) != 2 {
		t.Fatalf("expected exactly {id, jobTitle} to survive the round trip, got: %s", data)
	}
}
