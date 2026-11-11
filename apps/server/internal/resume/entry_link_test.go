package resume

import (
	"strings"
	"testing"
)

func TestValidEntryLink_FollowsTheSchemaLinkContract(t *testing.T) {
	base := "https://example.com/"
	for link, want := range map[string]bool{
		"":                          true,
		"https://example.com/Atlas": true,
		"mailto:ada@example.com":    true,
		"HTTPS://example.com":       false,
		"http://example.com":        false,
		base + strings.Repeat("a", 2048-len(base)): true,
		base + strings.Repeat("a", 2049-len(base)): false,
	} {
		if got := ValidEntryLink(link); got != want {
			t.Errorf("ValidEntryLink(%.40q) = %v, want %v", link, got, want)
		}
	}
}
