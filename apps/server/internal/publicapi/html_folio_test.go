package publicapi

import (
	stdhtml "html"
	"strconv"
	"strings"
	"testing"

	"danny.vn/aboutme/apps/server/internal/publicformat"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

func folioMarkup(attributes string) string {
	return `<div class="resume-folio" aria-hidden="true"` + attributes + `></div>`
}

func namedFolio(name, page string) string {
	return folioMarkup(` data-folio-name="` + stdhtml.EscapeString(name) + `" data-folio-page="` + page + `"`)
}

func unnamedFolio(page string) string {
	return folioMarkup(` data-folio-page="` + page + `"`)
}

func folioSequence(name string, total int) string {
	var out strings.Builder
	for index := 1; index <= total; index++ {
		page := strconv.Itoa(index) + " / " + strconv.Itoa(total)
		if name == "" {
			out.WriteString(unnamedFolio(page))
		} else {
			out.WriteString(namedFolio(name, page))
		}
	}
	return out.String()
}

func folioPage(t *testing.T, fullName, folios string) (string, publicresume.PublicResume, publicformat.JSONLDResult) {
	t.Helper()
	origin := mustPublicOrigin(t)
	resume := publicresume.PublicResume{Slug: "ada", Revision: "1", Document: publicresume.PublicResumeDocument{
		PersonalDetails: publicresume.PublicPersonalDetails{FullName: fullName}}}
	jsonLD, err := publicformat.JSONLD(resume, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	page := validHTML(stdhtml.EscapeString(fullName), "https://aboutme.example/ada", "1", "")
	fixture := folioSequence(fullName, 1)
	if !strings.Contains(page, fixture) {
		t.Fatalf("fixture lacks its folio %q", fixture)
	}
	return strings.Replace(page, fixture, folios, 1), resume, jsonLD
}

func TestPublicHTMLAcceptsFoliosInOrder(t *testing.T) {
	for _, test := range []struct {
		name, fullName string
		total          int
	}{
		{"one named sheet", "Ada", 1},
		{"three named sheets", "Ada", 3},
		{"no name", "", 4},
		{"separators in the name", "Ada · Lovelace / 2 / 3", 2},
		{"whitespace-only name", "   ", 2},
		{"escaped characters", `Ada "<&>" Lovelace`, 2},
		{"carriage return and line feed in the name", "Ada\r\nLovelace", 2},
		{"carriage return in the name", "Ada\rLovelace", 2},
		{"both line endings in the name", "Ada\r\n\rLovelace\n", 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			page, resume, jsonLD := folioPage(t, test.fullName, folioSequence(test.fullName, test.total))
			if rule := publicHTMLRejection([]byte(page), resume, mustPublicOrigin(t), jsonLD, false); rule != "" {
				t.Fatalf("folios rejected by %q", rule)
			}
		})
	}
}

func TestPublicHTMLRejectsBrokenFolios(t *testing.T) {
	separated := "Ada · Lovelace / 2"
	for _, test := range []struct {
		name, fullName, folios string
	}{
		{"missing folio", "Ada", ""},
		{"missing folio without a name", "", ""},
		{"page out of order", "Ada", namedFolio("Ada", "2 / 3") + namedFolio("Ada", "1 / 3") + namedFolio("Ada", "3 / 3")},
		{"repeated page", "Ada", namedFolio("Ada", "1 / 2") + namedFolio("Ada", "1 / 2")},
		{"total above the folio count", "Ada", namedFolio("Ada", "1 / 3") + namedFolio("Ada", "2 / 3")},
		{"total below the folio count", "Ada", namedFolio("Ada", "1 / 2") + namedFolio("Ada", "2 / 2") + namedFolio("Ada", "3 / 2")},
		{"page without spaces", "Ada", namedFolio("Ada", "1/1")},
		{"page with a padded number", "Ada", namedFolio("Ada", "01 / 1")},
		{"page with outer space", "Ada", namedFolio("Ada", " 1 / 1")},
		{"missing page", "Ada", folioMarkup(` data-folio-name="Ada"`)},
		{"name when fullName is empty", "", namedFolio("Ada", "1 / 1")},
		{"empty name when fullName is empty", "", folioMarkup(` data-folio-name="" data-folio-page="1 / 1"`)},
		{"name absent when fullName is set", "Ada", unnamedFolio("1 / 1")},
		{"name absent on a later sheet", "Ada", namedFolio("Ada", "1 / 2") + unnamedFolio("2 / 2")},
		{"name that differs", "Ada", namedFolio("Grace", "1 / 1")},
		{"name in another case", "Ada", namedFolio("ADA", "1 / 1")},
		{"name trimmed", " Ada ", namedFolio("Ada", "1 / 1")},
		{"separated name split", separated, namedFolio("Ada · Lovelace", "1 / 1")},
		{"separated name with its page moved in", separated, namedFolio(separated+" / 1", "1")},
		{"whitespace-only name absent", "   ", unnamedFolio("1 / 1")},
		{"whitespace-only name emptied", "   ", folioMarkup(` data-folio-name="" data-folio-page="1 / 1"`)},
		{"whitespace-only name shortened", "   ", namedFolio(" ", "1 / 1")},
		{"carriage return dropped from the name", "Ada\r\nLovelace", namedFolio("AdaLovelace", "1 / 1")},
		{"carriage return as a space", "Ada\rLovelace", namedFolio("Ada Lovelace", "1 / 1")},
		{"line feed doubled", "Ada\r\nLovelace", namedFolio("Ada\n\nLovelace", "1 / 1")},
	} {
		t.Run(test.name, func(t *testing.T) {
			page, resume, jsonLD := folioPage(t, test.fullName, test.folios)
			if rule := publicHTMLRejection([]byte(page), resume, mustPublicOrigin(t), jsonLD, false); rule != "folio" {
				t.Fatalf("rule = %q, want folio", rule)
			}
		})
	}
}

func TestPublicHTMLFindsFoliosByClassToken(t *testing.T) {
	page, resume, jsonLD := folioPage(t, "Ada", strings.Replace(namedFolio("Ada", "1 / 1"), `class="resume-folio"`, `class="x resume-folio"`, 1))
	if rule := publicHTMLRejection([]byte(page), resume, mustPublicOrigin(t), jsonLD, false); rule != "" {
		t.Fatalf("folio with another class token rejected by %q", rule)
	}
	page, resume, jsonLD = folioPage(t, "Ada", namedFolio("Ada", "1 / 1")+
		strings.Replace(namedFolio("Ada", "9 / 9"), `class="resume-folio"`, `class="resume-folio-note"`, 1))
	if rule := publicHTMLRejection([]byte(page), resume, mustPublicOrigin(t), jsonLD, false); rule != "" {
		t.Fatalf("an element without the folio class counted as a folio: %q", rule)
	}
}
