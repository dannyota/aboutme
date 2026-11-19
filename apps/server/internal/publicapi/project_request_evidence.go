package publicapi

import (
	"strings"
	"unicode"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

const evidenceInstantLayout = "2006-01-02T15:04:05Z"

func projectEvidence(items []publicresume.PublicEvidence) []directrender.ProjectEvidence {
	out := make([]directrender.ProjectEvidence, 0, len(items))
	for _, item := range items {
		out = append(out, directrender.ProjectEvidence{
			Kind: item.Kind, Title: evidenceText(item.Title), URL: item.URL, RepositoryName: evidenceText(item.RepositoryName),
			ItemDate: item.ItemDate.UTC().Format(evidenceInstantLayout), CheckedAt: item.CheckedAt.UTC().Format(evidenceInstantLayout),
			Confirmed: item.Confirmed,
		})
	}
	return out
}

func evidenceText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) {
			return ' '
		}
		return r
	}, text)
}
