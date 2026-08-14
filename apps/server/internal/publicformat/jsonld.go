package publicformat

import (
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

const JSONLDFormatVersion = 1

const BaseCSP = "default-src 'none'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; manifest-src 'self'; media-src 'none'; worker-src 'none'"

type JSONLDResult struct {
	JSON   []byte
	Script []byte
	CSP    string
}

func JSONLD(resume publicresume.PublicResume, origin publicresume.PublicOrigin, discoverable bool) (JSONLDResult, error) {
	if !discoverable {
		return JSONLDResult{CSP: BaseCSP}, nil
	}
	json := profilePageJSON(resume, origin)
	digest := sha256.Sum256(json)
	hash := base64.StdEncoding.EncodeToString(digest[:])
	return JSONLDResult{
		JSON:   json,
		Script: append(append([]byte("<script type=\"application/ld+json\">"), json...), []byte("</script>")...),
		CSP:    strings.Replace(BaseCSP, "script-src 'self';", "script-src 'self' 'sha256-"+hash+"';", 1),
	}, nil
}

func profilePageJSON(resume publicresume.PublicResume, origin publicresume.PublicOrigin) []byte {
	person := resume.Document.PersonalDetails
	var out strings.Builder
	out.WriteString(`{"@context":"https://schema.org","@type":"ProfilePage","url":`)
	jsonString(&out, origin.Resolve("/"+resume.Slug))
	out.WriteString(`,"name":`)
	jsonString(&out, person.FullName+" — Resume")
	out.WriteString(`,"inLanguage":`)
	jsonString(&out, resume.Lng)
	out.WriteString(`,"mainEntity":{"@type":"Person","name":`)
	jsonString(&out, person.FullName)
	if person.Headline != nil && strings.TrimSpace(*person.Headline) != "" {
		out.WriteString(`,"description":`)
		jsonString(&out, *person.Headline)
	}
	if person.Photo != nil {
		out.WriteString(`,"image":`)
		jsonString(&out, person.Photo.URL)
	}
	if sameAs := jsonLDSameAs(person.Details); len(sameAs) != 0 {
		out.WriteString(`,"sameAs":[`)
		for index, value := range sameAs {
			if index != 0 {
				out.WriteByte(',')
			}
			jsonString(&out, value)
		}
		out.WriteByte(']')
	}
	out.WriteString(`}}`)
	return []byte(out.String())
}

func jsonLDSameAs(details publicresume.PublicDetails) []string {
	if !details.Present() {
		return nil
	}
	seen := make(map[string]struct{})
	result := []string{}
	for _, detail := range details.Value() {
		if !isLinkContact(detail.Type) || !isHTTPSURL(detail.Value) {
			continue
		}
		if _, exists := seen[detail.Value]; exists {
			continue
		}
		seen[detail.Value] = struct{}{}
		result = append(result, detail.Value)
	}
	return result
}

func isHTTPSURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

func jsonString(out *strings.Builder, value string) {
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		case '<':
			out.WriteString(`\u003c`)
		case '>':
			out.WriteString(`\u003e`)
		case '&':
			out.WriteString(`\u0026`)
		case '\u2028':
			out.WriteString(`\u2028`)
		case '\u2029':
			out.WriteString(`\u2029`)
		default:
			if r >= 0 && r <= 0x1f {
				out.WriteString(`\u00`)
				const hex = "0123456789abcdef"
				out.WriteByte(hex[(r>>4)&0xf])
				out.WriteByte(hex[r&0xf])
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}
