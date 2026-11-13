package publicapi

// A Go copy of the web renderer's strict project envelope decoder,
// apps/web/server/utils/public-render/project-decode.ts and the field checks
// in envelope-fields.ts. Each check names the rule it applies, so a builder
// change the renderer would refuse fails here first. Types are exact: a
// value of another type fails its rule, as it does in TypeScript. Links and
// origins follow the WHATWG URL parser through rendererLink and
// whatwgHostPort, which renderer_link_test.go pins against Node's URL.
// Preview text uses the project page bounds agreed with the renderer.

import (
	"encoding/json"
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
		listURL := d.common(envelope) + "/projects"
		rows, ok := envelope["projects"].([]any)
		d.require(envelope["pageUrl"] == listURL, "list pageUrl")
		d.require(ok && len(rows) >= 1 && len(rows) <= 30, "list has 1 to 30 rows")
		seen := map[any]bool{}
		for _, row := range rows {
			fields := d.row(row, listURL)
			d.require(!seen[fields["slug"]], "row slugs are unique")
			seen[fields["slug"]] = true
		}
	case "project":
		d.keys(envelope, append(common, "project", "listUrl", "otherProjects"), []string{"entryLink"}, "envelope keys")
		listURL := d.common(envelope) + "/projects"
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
	origin, isString := envelope["canonicalOrigin"].(string)
	d.require(isString && decoderNormalizedOrigin(origin), "canonicalOrigin is a normalized origin")
	_, isBool := envelope["discoveryEnabled"].(bool)
	d.require(isBool, "discoveryEnabled")
	title, isString := envelope["pageTitle"].(string)
	d.require(isString && title != "" && len(utf16.Encode([]rune(title))) <= 4096, "pageTitle is 1 to 4096 UTF-16 units")
	favicon, isString := envelope["faviconHref"].(string)
	d.require(isString && (favicon == "" || decoderFaviconHref.MatchString(favicon) && len(favicon) <= len("data:image/svg+xml,")+2048), "faviconHref")

	resume := d.object(envelope["resume"], "resume")
	d.keys(resume, []string{"slug", "url", "ownerName", "lng", "uiLng", "colorScheme", "dateFormat"}, nil, "resume keys")
	slug, isString := resume["slug"].(string)
	d.require(isString && decoderSlug.MatchString(slug), "resume slug")
	d.require(resume["url"] == origin+"/"+slug, "resume url is origin/slug")
	owner, isString := resume["ownerName"].(string)
	d.require(isString && decoderBoundedText(owner, 2240) && !blankText(owner), "ownerName is non-blank and at most 2240 bytes")
	lng, isString := resume["lng"].(string)
	d.require(isString && len(lng) <= 35 && decoderResumeLng.MatchString(lng), "lng is a tag of at most 35 characters")
	primary, _, _ := strings.Cut(strings.ToLower(lng), "-")
	wantUI := "en"
	if primary == "vi" {
		wantUI = "vi"
	}
	d.require(resume["uiLng"] == wantUI, "uiLng follows lng's primary subtag")
	d.require(slices.Contains([]any{"light", "dark", "system"}, resume["colorScheme"]), "colorScheme")
	d.require(slices.Contains([]any{"MM/YYYY", "Mon YYYY", "YYYY"}, resume["dateFormat"]), "dateFormat")
	d.preview(d.object(envelope["preview"], "preview"), origin, slug)
	resumeURL, isString := resume["url"].(string)
	d.require(isString, "resume url is a string")
	return resumeURL
}

// preview checks the preview text against the project page bounds.
func (d *envelopeDecoder) preview(preview map[string]any, origin, slug string) {
	d.keys(preview, []string{"title", "description", "locale", "imageAlt"}, []string{"imageUrl"}, "preview keys")
	title, isString := preview["title"].(string)
	d.require(isString && title != "" && utf8.RuneCountInString(title) <= 683 && len(title) <= 2727, "preview title is 1 to 683 code points and 2727 bytes")
	description, isString := preview["description"].(string)
	d.require(isString && utf8.RuneCountInString(description) <= 300 && len(description) <= 1200, "preview description is at most 300 code points and 1200 bytes")
	alt, isString := preview["imageAlt"].(string)
	d.require(isString && decoderBoundedText(alt, 1024), "preview imageAlt is 1 to 1024 bytes")
	locale, isString := preview["locale"].(string)
	d.require(isString && (locale == "" || decoderPreviewLocale.MatchString(locale)), "preview locale")
	if image, present := preview["imageUrl"]; present {
		imageURL, isImage := image.(string)
		version, found := strings.CutPrefix(imageURL, origin+"/api/v1/public/resumes/"+slug+"/og/")
		d.require(isImage && found && decoderCardVersion.MatchString(version), "preview imageUrl is this resume's card")
	}
}

// text checks the fields a list row and a project page share.
func (d *envelopeDecoder) text(project map[string]any, listURL string) {
	slug, isString := project["slug"].(string)
	d.require(isString && len(slug) >= 2 && len(slug) <= 40 && decoderProjectSlug.MatchString(slug), "project slug")
	d.require(project["url"] == listURL+"/"+slug, "project url is listUrl/slug")
	if lang, present := project["lang"]; present {
		value, isLang := lang.(string)
		d.require(isLang && decoderProjectLang.MatchString(value), "lang is a primary subtag")
	}
	name, isString := project["name"].(string)
	d.require(isString && decoderText(name, 120) && !blankText(name), "name is non-blank and at most 120 code points")
	d.optionalText(project, "role", 120)
	d.optionalText(project, "summary", 300)
	if dates, present := project["dates"]; present {
		d.dates(dates)
	}
}

func (d *envelopeDecoder) row(value any, listURL string) map[string]any {
	row := d.object(value, "row")
	d.keys(row, []string{"slug", "url", "name", "technologies"}, []string{"lang", "role", "dates", "summary", "moreTechnologies"}, "row keys")
	d.text(row, listURL)
	technologies := d.technologies(row["technologies"], 6)
	if more, present := row["moreTechnologies"]; present {
		count, isNumber := more.(float64)
		d.require(isNumber && count == float64(int(count)) && count >= 1 && count <= 14 && technologies == 6, "moreTechnologies is 1 to 14 beside six technologies")
	}
	return row
}

func (d *envelopeDecoder) detail(value any, listURL string) map[string]any {
	project := d.object(value, "project")
	d.keys(project, []string{"slug", "url", "name", "technologies"},
		[]string{"lang", "role", "dates", "summary", "outcome", "description", "repositoryUrl", "demoUrl"}, "project keys")
	d.text(project, listURL)
	d.technologies(project["technologies"], 20)
	d.optionalText(project, "outcome", 600)
	if description, present := project["description"]; present {
		text, isString := description.(string)
		d.require(isString && decoderBoundedText(text, 16384), "description is 1 to 16384 bytes")
	}
	for _, key := range []string{"repositoryUrl", "demoUrl"} {
		if link, present := project[key]; present {
			d.require(decoderLink(link), key+" is an https link")
		}
	}
	return project
}

func (d *envelopeDecoder) technologies(value any, limit int) int {
	names, isList := value.([]any)
	d.require(isList && len(names) <= limit, "technologies count")
	for _, name := range names {
		text, isString := name.(string)
		d.require(isString && decoderText(text, 40), "technology is a string of at most 40 code points")
	}
	return len(names)
}

// optionalText requires an optional text to be absent, or a string, not
// empty and within its limit.
func (d *envelopeDecoder) optionalText(value map[string]any, key string, limit int) {
	if field, present := value[key]; present {
		text, isString := field.(string)
		d.require(isString && text != "" && decoderText(text, limit), key+" is omitted or 1 to its limit")
	}
}

func (d *envelopeDecoder) dates(value any) {
	dates := d.object(value, "dates")
	d.keys(dates, []string{"start", "end", "present"}, nil, "dates keys")
	present, isBool := dates["present"].(bool)
	d.require(isBool && decoderYearMonth(dates["start"]), "dates start")
	if present {
		d.require(dates["end"] == nil, "a present range has a null end")
	} else {
		d.require(decoderYearMonth(dates["end"]), "a completed range has an end")
	}
}

// object requires an object; another type fails and reads as empty.
func (d *envelopeDecoder) object(value any, rule string) map[string]any {
	object, isObject := value.(map[string]any)
	d.require(isObject, rule+" is an object")
	return object
}

func decoderYearMonth(value any) bool {
	month, isObject := value.(map[string]any)
	if !isObject {
		return false
	}
	for key := range month {
		if key != "y" && key != "m" {
			return false
		}
	}
	year, isNumber := month["y"].(float64)
	if !isNumber || year != float64(int(year)) || year < 1900 || year > 2100 {
		return false
	}
	if m, present := month["m"]; present {
		number, isNumber := m.(float64)
		return isNumber && number == float64(int(number)) && number >= 1 && number <= 12
	}
	return true
}

func decoderText(value string, maxCodePoints int) bool {
	return utf8.RuneCountInString(value) <= maxCodePoints
}

func decoderBoundedText(value string, maxBytes int) bool {
	return value != "" && len(value) <= maxBytes
}

// decoderLink is project-decode.ts externalLink.
func decoderLink(value any) bool {
	link, isString := value.(string)
	return isString && rendererLink(link)
}

// decoderNormalizedOrigin is envelope-fields.ts normalizedOrigin: an ASCII
// string of at most 512 bytes that is exactly its own http or https origin,
// with no user, path, query or fragment.
func decoderNormalizedOrigin(value any) bool {
	origin, isString := value.(string)
	if !isString || len(origin) > 512 || strings.IndexFunc(origin, func(r rune) bool { return r > 0x7f }) >= 0 {
		return false
	}
	scheme, authority, found := strings.Cut(origin, "://")
	if !found || (scheme != "http" && scheme != "https") || strings.ContainsAny(authority, "/\\?#@") {
		return false
	}
	host, port, ok := whatwgHostPort(authority)
	if !ok {
		return false
	}
	serialized := scheme + "://" + host
	defaultPort := scheme == "https" && port == "443" || scheme == "http" && port == "80"
	if port != "" && !defaultPort {
		serialized += ":" + port
	}
	return serialized == origin
}
