package previewmeta

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/rivo/uniseg"
)

// textCases holds the text-rule cases in testdata/text-cases.json. They are
// plain data so another implementation of the rules can check against them.
type textCases struct {
	Normalize []struct {
		Name  string `json:"name"`
		Input string `json:"input"`
		Want  string `json:"want"`
	} `json:"normalize"`
	Scrub []struct {
		Name     string   `json:"name"`
		Input    string   `json:"input"`
		Contacts []string `json:"contacts"`
		Want     string   `json:"want"`
	} `json:"scrub"`
	Cut []struct {
		Name  string `json:"name"`
		Input string `json:"input"`
		Want  string `json:"want"`
	} `json:"cut"`
	Locale []struct {
		Lng  string `json:"lng"`
		Want string `json:"want"`
	} `json:"locale"`
}

func loadTextCases(t *testing.T) textCases {
	t.Helper()
	source, err := os.ReadFile("testdata/text-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases textCases
	if err := json.Unmarshal(source, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases.Normalize) == 0 || len(cases.Scrub) == 0 || len(cases.Cut) == 0 || len(cases.Locale) == 0 {
		t.Fatal("text cases are missing a rule")
	}
	return cases
}

func TestNormalizeCases(t *testing.T) {
	for _, test := range loadTextCases(t).Normalize {
		t.Run(test.Name, func(t *testing.T) {
			if got := Normalize(test.Input); got != test.Want {
				t.Fatalf("Normalize(%q) = %q, want %q", test.Input, got, test.Want)
			}
		})
	}
}

func TestScrubCases(t *testing.T) {
	for _, test := range loadTextCases(t).Scrub {
		t.Run(test.Name, func(t *testing.T) {
			if got := Scrub(test.Input, test.Contacts); got != test.Want {
				t.Fatalf("Scrub(%q) = %q, want %q", test.Input, got, test.Want)
			}
		})
	}
}

func TestCutCases(t *testing.T) {
	for _, test := range loadTextCases(t).Cut {
		t.Run(test.Name, func(t *testing.T) {
			got := Cut(test.Input)
			if got != test.Want {
				t.Fatalf("Cut() = %q, want %q", got, test.Want)
			}
			if uniseg.GraphemeClusterCount(got) > MaxDescriptionGraphemes || len(got) > MaxDescriptionBytes {
				t.Fatalf("Cut() = %d graphemes, %d bytes", uniseg.GraphemeClusterCount(got), len(got))
			}
		})
	}
}

func TestLocaleCases(t *testing.T) {
	for _, test := range loadTextCases(t).Locale {
		t.Run(test.Lng, func(t *testing.T) {
			if got := Locale(test.Lng); got != test.Want {
				t.Fatalf("Locale(%q) = %q, want %q", test.Lng, got, test.Want)
			}
		})
	}
}

func TestCutNeverSplitsAGraphemeCluster(t *testing.T) {
	// A Vietnamese letter in decomposed form is one cluster of three code
	// points; a cut through it would leave a bare combining mark.
	decomposed := "e\u0302\u0301"
	input := ""
	for range 200 {
		input += decomposed
	}
	got := Cut(input)
	want := ""
	for range MaxDescriptionGraphemes - 1 {
		want += decomposed
	}
	want += ellipsis
	if got != want {
		t.Fatalf("Cut() = %q, want %q", got, want)
	}
}

func TestCutAtTheLimitKeepsTheText(t *testing.T) {
	for _, length := range []int{MaxDescriptionGraphemes - 1, MaxDescriptionGraphemes} {
		input := ""
		for range length {
			input += "ă"
		}
		if got := Cut(input); got != input {
			t.Fatalf("Cut() of %d graphemes = %q, want it unchanged", length, got)
		}
	}
	over := ""
	for range MaxDescriptionGraphemes + 1 {
		over += "ă"
	}
	if got := Cut(over); uniseg.GraphemeClusterCount(got) != MaxDescriptionGraphemes {
		t.Fatalf("Cut() of %d graphemes = %d graphemes", MaxDescriptionGraphemes+1, uniseg.GraphemeClusterCount(got))
	}
}

func TestRichTextPlainJoinsBlocksWithSpaces(t *testing.T) {
	got := richTextPlain("<p>First&nbsp;line<br>second <strong>bold</strong>&amp;co</p><ul><li>one</li><li>two</li></ul>")
	if want := "First line second bold&co one two"; got != want {
		t.Fatalf("richTextPlain() = %q, want %q", got, want)
	}
}
