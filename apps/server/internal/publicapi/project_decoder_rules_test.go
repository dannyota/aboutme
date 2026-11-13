package publicapi

// A Go mirror of the web renderer's strict project envelope decoder,
// apps/web/server/utils/public-render/project-decode.ts and the field checks
// in envelope-fields.ts. Each check names the rule it applies, so a builder
// change that the renderer would refuse fails here first.

import (
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

var (
	// RE2 caps a repeat count at 1000, so the 2048 bound is checked apart.
	decoderFaviconHref   = regexp.MustCompile(`^data:image/svg\+xml,[A-Za-z0-9._~%-]+$`)
	decoderPreviewLocale = regexp.MustCompile(`^[a-z]{2,3}_[A-Z]{2}$`)
	decoderSlug          = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	decoderProjectSlug   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	decoderProjectLang   = regexp.MustCompile(`^[a-z]{2,8}$`)
	decoderResumeLng     = regexp.MustCompile(`^[A-Za-z]{1,8}(?:-[A-Za-z0-9]{1,8})*$`)
	decoderCardVersion   = regexp.MustCompile(`^[0-9a-f]{16}\.png$`)
)

// envelopeDecoder collects every decoder rule one envelope breaks.
type envelopeDecoder struct{ broken []string }

func (d *envelopeDecoder) require(ok bool, rule string) {
	if !ok {
		d.broken = append(d.broken, rule)
	}
}

// decodeProjectEnvelopeRules runs the decoder for the envelope's mode over
// its JSON encoding and returns the broken rules.
func decodeProjectEnvelopeRules(t *testing.T, request any) []string {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	d := &envelopeDecoder{}
	common := []string{"mode", "canonicalOrigin", "pageUrl", "discoveryEnabled", "pageTitle", "faviconHref", "preview", "resume"}
	switch envelope["mode"] {
	case "project-list":
		d.keys(envelope, append(common, "projects"), nil, "envelope keys")
		resume := d.common(envelope)
		listURL := resume + "/projects"
		rows, ok := envelope["projects"].([]any)
		d.require(envelope["pageUrl"] == listURL, "list pageUrl")
		d.require(ok && len(rows) >= 1 && len(rows) <= 30, "list has 1 to 30 rows")
		seen := map[any]bool{}
		for _, row := range rows {
			d.row(row, listURL)
			if fields, isObject := row.(map[string]any); isObject {
				d.require(!seen[fields["slug"]], "row slugs are unique")
				seen[fields["slug"]] = true
			}
		}
	case "project":
		d.keys(envelope, append(common, "project", "listUrl", "otherProjects"), []string{"entryLink"}, "envelope keys")
		resume := d.common(envelope)
		listURL := resume + "/projects"
		project := d.detail(envelope["project"], listURL)
		d.require(envelope["listUrl"] == listURL, "listUrl")
		d.require(envelope["pageUrl"] == project["url"], "pageUrl is the project url")
		_, isBool := envelope["otherProjects"].(bool)
		d.require(isBool, "otherProjects")
		if entry, present := envelope["entryLink"]; present {
			d.require(decoderLink(entry), "entryLink is an https link")
			d.require(entry != project["repositoryUrl"] && entry != project["demoUrl"], "entryLink differs from both buttons")
		}
	default:
		d.require(false, "mode")
	}
	return d.broken
}

// keys requires every required key and allows only the optional ones.
func (d *envelopeDecoder) keys(value map[string]any, required, optional []string, rule string) {
	for _, key := range required {
		_, present := value[key]
		d.require(present, rule+": "+key+" present")
	}
	for key := range value {
		d.require(slices.Contains(required, key) || slices.Contains(optional, key), rule+": "+key+" allowed")
	}
}

// common checks the head fields and the resume, and returns the resume url.
func (d *envelopeDecoder) common(envelope map[string]any) string {
	origin := decoderString(envelope["canonicalOrigin"])
	parsed, err := url.Parse(origin)
	d.require(err == nil && len(origin) <= 512 && parsed.Scheme+"://"+parsed.Host == origin && parsed.Path == "", "canonicalOrigin is a normalized origin")
	_, isBool := envelope["discoveryEnabled"].(bool)
	d.require(isBool, "discoveryEnabled")
	title := decoderString(envelope["pageTitle"])
	d.require(title != "" && len(utf16.Encode([]rune(title))) <= 4096, "pageTitle is 1 to 4096 UTF-16 units")
	favicon := decoderString(envelope["faviconHref"])
	d.require(favicon == "" || decoderFaviconHref.MatchString(favicon) && len(favicon) <= len("data:image/svg+xml,")+2048, "faviconHref")

	resume := decoderObject(envelope["resume"])
	d.keys(resume, []string{"slug", "url", "ownerName", "lng", "uiLng", "colorScheme", "dateFormat"}, nil, "resume keys")
	slug := decoderString(resume["slug"])
	d.require(decoderSlug.MatchString(slug), "resume slug")
	d.require(resume["url"] == origin+"/"+slug, "resume url is origin/slug")
	owner := decoderString(resume["ownerName"])
	d.require(decoderBoundedText(owner, 2240) && strings.TrimSpace(owner) != "", "ownerName is non-blank and at most 2240 bytes")
	lng := decoderString(resume["lng"])
	d.require(len(lng) <= 35 && decoderResumeLng.MatchString(lng), "lng is a tag of at most 35 characters")
	primary, _, _ := strings.Cut(strings.ToLower(lng), "-")
	wantUI := "en"
	if primary == "vi" {
		wantUI = "vi"
	}
	d.require(resume["uiLng"] == wantUI, "uiLng follows lng's primary subtag")
	d.require(slices.Contains([]any{"light", "dark", "system"}, resume["colorScheme"]), "colorScheme")
	d.require(slices.Contains([]any{"MM/YYYY", "Mon YYYY", "YYYY"}, resume["dateFormat"]), "dateFormat")

	preview := decoderObject(envelope["preview"])
	d.keys(preview, []string{"title", "description", "locale", "imageAlt"}, []string{"imageUrl"}, "preview keys")
	previewTitle := decoderString(preview["title"])
	d.require(decoderBoundedText(previewTitle, 2240), "preview title is 1 to 2240 bytes")
	description, isString := preview["description"].(string)
	d.require(isString && len(description) <= 1024, "preview description is at most 1024 bytes")
	alt := decoderString(preview["imageAlt"])
	d.require(decoderBoundedText(alt, 1024), "preview imageAlt is 1 to 1024 bytes")
	locale := decoderString(preview["locale"])
	d.require(locale == "" || decoderPreviewLocale.MatchString(locale), "preview locale")
	if image, present := preview["imageUrl"]; present {
		imageURL := decoderString(image)
		version, found := strings.CutPrefix(imageURL, origin+"/api/v1/public/resumes/"+slug+"/og/")
		d.require(found && decoderCardVersion.MatchString(version), "preview imageUrl is this resume's card")
	}
	resumeURL := decoderString(resume["url"])
	return resumeURL
}

// text checks the fields a list row and a project page share.
func (d *envelopeDecoder) text(project map[string]any, listURL string) {
	slug := decoderString(project["slug"])
	d.require(len(slug) >= 2 && len(slug) <= 40 && decoderProjectSlug.MatchString(slug), "project slug")
	d.require(project["url"] == listURL+"/"+slug, "project url is listUrl/slug")
	if lang, present := project["lang"]; present {
		value := decoderString(lang)
		d.require(decoderProjectLang.MatchString(value), "lang is a primary subtag")
	}
	name := decoderString(project["name"])
	d.require(decoderText(name, 120) && strings.TrimSpace(name) != "", "name is non-blank and at most 120 code points")
	d.optionalText(project, "role", 120)
	d.optionalText(project, "summary", 300)
	if dates, present := project["dates"]; present {
		d.dates(dates)
	}
}

func (d *envelopeDecoder) row(value any, listURL string) {
	row := decoderObject(value)
	d.keys(row, []string{"slug", "url", "name", "technologies"}, []string{"lang", "role", "dates", "summary", "moreTechnologies"}, "row keys")
	d.text(row, listURL)
	technologies := d.technologies(row["technologies"], 6)
	if more, present := row["moreTechnologies"]; present {
		count := decoderNumber(more)
		d.require(count == float64(int(count)) && count >= 1 && count <= 14 && technologies == 6, "moreTechnologies is 1 to 14 beside six technologies")
	}
}

func (d *envelopeDecoder) detail(value any, listURL string) map[string]any {
	project := decoderObject(value)
	d.keys(project, []string{"slug", "url", "name", "technologies"},
		[]string{"lang", "role", "dates", "summary", "outcome", "description", "repositoryUrl", "demoUrl"}, "project keys")
	d.text(project, listURL)
	d.technologies(project["technologies"], 20)
	d.optionalText(project, "outcome", 600)
	if description, present := project["description"]; present {
		text := decoderString(description)
		d.require(decoderBoundedText(text, 16384), "description is 1 to 16384 bytes")
	}
	for _, key := range []string{"repositoryUrl", "demoUrl"} {
		if link, present := project[key]; present {
			d.require(decoderLink(link), key+" is an https link")
		}
	}
	return project
}

func (d *envelopeDecoder) technologies(value any, limit int) int {
	names, ok := value.([]any)
	d.require(ok && len(names) <= limit, "technologies count")
	for _, name := range names {
		text := decoderString(name)
		d.require(decoderText(text, 40), "technology is at most 40 code points")
	}
	return len(names)
}

// optionalText requires an optional text to be absent, or present, not
// empty and within its limit.
func (d *envelopeDecoder) optionalText(value map[string]any, key string, limit int) {
	if field, present := value[key]; present {
		text := decoderString(field)
		d.require(text != "" && decoderText(text, limit), key+" is omitted or 1 to its limit")
	}
}

func (d *envelopeDecoder) dates(value any) {
	dates := decoderObject(value)
	d.keys(dates, []string{"start", "end", "present"}, nil, "dates keys")
	present, isBool := dates["present"].(bool)
	d.require(isBool && decoderYearMonth(dates["start"]), "dates start")
	if present {
		d.require(dates["end"] == nil, "a present range has a null end")
	} else {
		d.require(decoderYearMonth(dates["end"]), "a completed range has an end")
	}
}

func decoderYearMonth(value any) bool {
	month, ok := value.(map[string]any)
	if !ok {
		return false
	}
	year := decoderNumber(month["y"])
	for key := range month {
		if key != "y" && key != "m" {
			return false
		}
	}
	if m, present := month["m"]; present {
		number := decoderNumber(m)
		if number < 1 || number > 12 || number != float64(int(number)) {
			return false
		}
	}
	return year >= 1900 && year <= 2100 && year == float64(int(year))
}

func decoderText(value string, maxCodePoints int) bool {
	return utf8.RuneCountInString(value) <= maxCodePoints
}

func decoderBoundedText(value string, maxBytes int) bool {
	return value != "" && len(value) <= maxBytes
}

func decoderLink(value any) bool {
	link, ok := value.(string)
	if !ok || !strings.HasPrefix(link, "https://") || utf8.RuneCountInString(link) > 2048 {
		return false
	}
	parsed, err := url.Parse(link)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != ""
}

// decoderString, decoderObject, decoderNumber and decoderList read a
// decoded JSON value as one type, or its zero value, which every rule then
// rejects.
func decoderString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func decoderObject(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	return nil
}

func decoderNumber(value any) float64 {
	if number, ok := value.(float64); ok {
		return number
	}
	return 0
}
