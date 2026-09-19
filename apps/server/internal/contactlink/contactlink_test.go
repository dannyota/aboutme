package contactlink

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The corpus is shared with the web renderer tests, so the Go and TS rules
// agree on every case (docs/adr/0043-email-and-phone-links.md).
func TestHrefCorpus(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "contact-link-corpus.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Type  string  `json:"type"`
			Value string  `json:"value"`
			Href  *string `json:"href"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) == 0 {
		t.Fatal("empty corpus")
	}
	for _, test := range corpus.Cases {
		got, ok := Href(test.Type, test.Value)
		switch {
		case test.Href == nil && ok:
			t.Errorf("Href(%s, %q) = %q, want text", test.Type, test.Value, got)
		case test.Href != nil && (!ok || got != *test.Href):
			t.Errorf("Href(%s, %q) = %q, %v, want %q", test.Type, test.Value, got, ok, *test.Href)
		}
	}
}
