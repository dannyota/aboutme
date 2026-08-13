package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPersonalDetailsMarshalPreservesEmptyDetailsPresence(t *testing.T) {
	empty, err := json.Marshal(PersonalDetails{Details: []PersonalDetail{}})
	if err != nil {
		t.Fatalf("marshal explicit empty details: %v", err)
	}
	if !strings.Contains(string(empty), `"details":[]`) {
		t.Fatalf("explicit empty details marshaled as %s, want details:[]", empty)
	}

	absent, err := json.Marshal(PersonalDetails{})
	if err != nil {
		t.Fatalf("marshal absent details: %v", err)
	}
	if strings.Contains(string(absent), `"details"`) {
		t.Fatalf("absent details marshaled as %s, want property omitted", absent)
	}
}
