package sanitize

import (
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/sanitize/sanitizetest"
	schemagen "github.com/dannyota/aboutme/packages/schema/gen/go"
)

func TestRichTextHostileCorpusConformance(t *testing.T) {
	t.Parallel()
	for _, row := range schemagen.HostileCorpus {
		row := row
		t.Run(row.ID, func(t *testing.T) {
			t.Parallel()
			if err := sanitizetest.AssertNeutralized(RichText(row.Payload)); err != nil {
				t.Fatalf("RichText(%q): %v", row.Payload, err)
			}
		})
	}
}
