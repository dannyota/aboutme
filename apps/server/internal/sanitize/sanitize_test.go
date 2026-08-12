package sanitize

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	schemagen "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/sanitize/sanitizetest"
)

func TestRichTextPreservesAllowedContent(t *testing.T) {
	t.Parallel()

	source := `<p>One<br><strong>two</strong><em>three</em><u>four</u></p>` +
		`<ol><li>five</li></ol><ul><li>six</li></ul>` +
		`<a href="https://example.com" target="_blank">seven</a>`
	got := RichText(source)
	if err := sanitizetest.AssertNeutralized(got); err != nil {
		t.Fatalf("RichText allowed content: %v\noutput: %s", err, got)
	}
	if got != `<p>One<br/><strong>two</strong><em>three</em><u>four</u></p>`+
		`<ol><li>five</li></ol><ul><li>six</li></ul>`+
		`<a href="https://example.com" target="_blank" rel="noopener noreferrer">seven</a>` {
		t.Fatalf("RichText allowed content = %q", got)
	}
}

func TestRichTextNormalizesAnchorAttributes(t *testing.T) {
	t.Parallel()
	want := `<a href="https://example.com" rel="noopener noreferrer">safe</a>`
	got := RichText(`<a href="https://example.com" rel="opener" target="other">safe</a>`)
	if got != want {
		t.Fatalf("RichText anchor = %q, want %q", got, want)
	}
}

func TestRichTextIsIdempotent(t *testing.T) {
	t.Parallel()
	inputs := []string{
		`<p>safe <strong>content</strong></p>`,
		`<a href="mailto:user@example.com">mail</a>`,
		`<a href="tel:+12025550123">call</a>`,
	}
	for _, row := range schemagen.HostileCorpus {
		inputs = append(inputs, row.Payload)
	}
	for _, input := range inputs {
		once := RichText(input)
		if twice := RichText(once); twice != once {
			t.Errorf("RichText is not idempotent for %q: once %q, twice %q", input, once, twice)
		}
	}
}

func TestRichTextMalformedInputNeverPanics(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"<", "<p", "<a href='https://example.com", "\x00<script>"} {
		if err := sanitizetest.AssertNeutralized(RichText(input)); err != nil {
			t.Errorf("RichText(%q): %v", input, err)
		}
	}
}

func FuzzRichText(f *testing.F) {
	for _, row := range schemagen.HostileCorpus {
		f.Add(row.Payload)
	}
	f.Add(`<p>safe <a href="https://example.com">link</a></p>`)
	f.Fuzz(func(t *testing.T, input string) {
		once := RichText(input)
		if err := sanitizetest.AssertNeutralized(once); err != nil {
			t.Fatalf("RichText(%q): %v", input, err)
		}
		if twice := RichText(once); twice != once {
			t.Fatalf("RichText not idempotent: once %q, twice %q", once, twice)
		}
	})
}

func TestCorpusOutputGolden(t *testing.T) {
	outputs := make(map[string]string, len(schemagen.HostileCorpus))
	for _, row := range schemagen.HostileCorpus {
		outputs[row.ID] = RichText(row.Payload)
	}
	data, err := json.MarshalIndent(outputs, "", "  ")
	if err != nil {
		t.Fatalf("marshal corpus output: %v", err)
	}
	data = append(data, '\n')

	path := filepath.Join("testdata", "corpus-output.golden.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o755); mkdirErr != nil {
			t.Fatalf("create golden directory: %v", mkdirErr)
		}
		if writeErr := os.WriteFile(path, data, 0o644); writeErr != nil {
			t.Fatalf("write golden: %v", writeErr)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(want) != string(data) {
		t.Fatalf("corpus output differs; run UPDATE_GOLDEN=1 go test ./internal/sanitize -run '^TestCorpusOutputGolden$' after reviewing the sanitizer change")
	}
}
