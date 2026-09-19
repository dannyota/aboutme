package publicpage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type corpusCase struct {
	Input  string   `json:"input"`
	Stored *string  `json:"stored"`
	Codes  []string `json:"codes"`
}

type publicPageCorpus struct {
	Titles       []corpusCase `json:"titles"`
	Emoji        []corpusCase `json:"emoji"`
	FaviconHrefs []struct {
		Emoji string `json:"emoji"`
		Href  string `json:"href"`
	} `json:"faviconHrefs"`
}

// The corpus is shared with apps/web/test/public-page, so the editor's
// pre-validation and the server agree on every hostile case.
func loadCorpus(t *testing.T) publicPageCorpus {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "public-page-corpus.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus publicPageCorpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	return corpus
}

func checkCase(t *testing.T, got Result, want corpusCase) {
	t.Helper()
	if !slices.Equal(got.Codes, want.Codes) && (len(got.Codes) != 0 || len(want.Codes) != 0) {
		t.Fatalf("%q: codes = %v, want %v", want.Input, got.Codes, want.Codes)
	}
	if len(want.Codes) != 0 {
		return
	}
	switch {
	case want.Stored == nil && got.Value != nil:
		t.Fatalf("%q: stored %q, want cleared", want.Input, *got.Value)
	case want.Stored != nil && (got.Value == nil || *got.Value != *want.Stored):
		t.Fatalf("%q: stored %v, want %q", want.Input, got.Value, *want.Stored)
	}
}

func TestNormalizeTitleCorpus(t *testing.T) {
	for _, test := range loadCorpus(t).Titles {
		checkCase(t, NormalizeTitle(test.Input), test)
	}
}

func TestNormalizeFaviconEmojiCorpus(t *testing.T) {
	for _, test := range loadCorpus(t).Emoji {
		checkCase(t, NormalizeFaviconEmoji(test.Input), test)
	}
}

func TestFaviconHrefCorpus(t *testing.T) {
	for _, test := range loadCorpus(t).FaviconHrefs {
		if got := FaviconHref(test.Emoji); got != test.Href {
			t.Fatalf("FaviconHref(%q) = %q, want %q", test.Emoji, got, test.Href)
		}
	}
}

func TestFaviconHrefStaysInsideItsAttribute(t *testing.T) {
	href := FaviconHref("\U0001F680")
	if !strings.HasPrefix(href, "data:image/svg+xml,") {
		t.Fatalf("href = %q", href)
	}
	for _, forbidden := range []string{`"`, "'", "<", ">", "&", " ", "\\"} {
		if strings.Contains(strings.TrimPrefix(href, "data:image/svg+xml,"), forbidden) {
			t.Fatalf("href carries %q unescaped: %s", forbidden, href)
		}
	}
}

func TestEffectiveTitle(t *testing.T) {
	custom := "Danny from aboutme.vn"
	if got := EffectiveTitle(&custom, "Danny"); got != custom {
		t.Fatalf("custom title = %q", got)
	}
	if got := EffectiveTitle(nil, "Ada Lovelace"); got != "Ada Lovelace — Resume" {
		t.Fatalf("default title = %q", got)
	}
	empty := ""
	if got := EffectiveTitle(&empty, "Ada"); got != "Ada — Resume" {
		t.Fatalf("empty title = %q", got)
	}
}

func TestExtendedPictographicTableIsSortedAndDisjoint(t *testing.T) {
	for index, entry := range extendedPictographic {
		if entry[0] > entry[1] {
			t.Fatalf("range %d is reversed: %X..%X", index, entry[0], entry[1])
		}
		if index > 0 && extendedPictographic[index-1][1]+1 >= entry[0] {
			t.Fatalf("range %d overlaps or touches its predecessor", index)
		}
	}
	for _, r := range []rune{0x1F680, 0x2764, 0x00A9} {
		if !isExtendedPictographic(r) {
			t.Fatalf("%X is Extended_Pictographic", r)
		}
	}
	for _, r := range []rune{'a', '1', 0x1F1FB, 0x1F3FD, 0x200D} {
		if isExtendedPictographic(r) {
			t.Fatalf("%X is not Extended_Pictographic", r)
		}
	}
}
