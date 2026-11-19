package publicapi

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

var (
	decoderRepositoryURL  = regexp.MustCompile(`^https://github\.com/[^/?#\t\n\v\f\r ]+/[^/?#\t\n\v\f\r ]+$`)
	decoderPullRequestURL = regexp.MustCompile(`^https://github\.com/[^/?#\t\n\v\f\r ]+/[^/?#\t\n\v\f\r ]+/pull/[1-9][0-9]*$`)
	decoderReleaseURL     = regexp.MustCompile(`^https://github\.com/[^/?#\t\n\v\f\r ]+/[^/?#\t\n\v\f\r ]+/releases/tag/[^/?#\t\n\v\f\r ]+$`)
	decoderInstant        = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`)
)

func decoderEvidenceGrammar(kind any) (*regexp.Regexp, bool) {
	switch kind {
	case "repository":
		return decoderRepositoryURL, true
	case "pullRequest":
		return decoderPullRequestURL, true
	case "release":
		return decoderReleaseURL, true
	default:
		return nil, false
	}
}

func (d *envelopeDecoder) evidence(value any) {
	items, isList := value.([]any)
	d.require(isList && len(items) <= 10, "evidence is an array of at most 10 items")
	for _, value := range items {
		item := d.object(value, "evidence item")
		d.keys(item, []string{"kind", "title", "url", "repositoryName", "itemDate", "checkedAt", "confirmed"}, nil, "evidence keys")
		grammar, known := decoderEvidenceGrammar(item["kind"])
		d.require(known, "evidence kind is repository, pullRequest or release")
		url, isString := item["url"].(string)
		d.require(isString && known && decoderStorable(url) && utf8.RuneCountInString(url) <= 300 && grammar.MatchString(url),
			"evidence url follows its kind's grammar in at most 300 code points")
		for _, key := range []string{"title", "repositoryName"} {
			text, isString := item[key].(string)
			d.require(isString && decoderEvidenceText(text), "evidence "+key+" is 1 to 200 code points with no Cc, Cf, Zl or Zp character")
		}
		for _, key := range []string{"itemDate", "checkedAt"} {
			d.require(decoderEvidenceInstant(item[key]), "evidence "+key+" is a UTC instant at second precision")
		}
		_, isBool := item["confirmed"].(bool)
		d.require(isBool, "evidence confirmed is a boolean")
	}
}

func decoderEvidenceText(text string) bool {
	return text != "" && decoderText(text, 200) &&
		!slices.ContainsFunc([]rune(text), func(r rune) bool { return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) })
}

func decoderEvidenceInstant(value any) bool {
	instant, isString := value.(string)
	if !isString || !decoderInstant.MatchString(instant) {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, instant)
	return err == nil && parsed.Format("2006-01-02T15:04:05Z") == instant
}

func boundedEvidence() publicresume.PublicProject {
	available := limitProject("evidence-bounds", 20, nil)
	prefix := "https://github.com/octocat/hello/releases/tag/"
	for i := range 10 {
		item := publicresume.PublicEvidence{
			Kind: project.EvidenceRelease, Title: wideText(200), RepositoryName: strings.Repeat("é", 200),
			URL:      prefix + strings.Repeat("t", 300-len(prefix)),
			ItemDate: time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC), CheckedAt: time.Date(9999, time.December, 31, 23, 59, 59, 999, time.UTC),
			Confirmed: i%2 == 0,
		}
		if i == 9 {
			item.Kind, item.URL = project.EvidencePullRequest, "https://github.com/"+strings.Repeat("o", 300-len("https://github.com//p/pull/9"))+"/p/pull/9"
		}
		available.Evidence = append(available.Evidence, item)
	}
	return available
}

func TestProjectDecoderRules_RefuseEachUnsafeEvidenceCharacter(t *testing.T) {
	page, err := buildProjectRequest(projectTestInput(t, projectTestSnapshot("en", "Ada", nil, schema.MmYyyy, false), 1, boundedEvidence()))
	if err != nil {
		t.Fatal(err)
	}
	if broken := decodeProjectEnvelopeRules(t, page); len(broken) != 0 {
		t.Fatalf("ten items at every bound break the decoder copy: %q", broken)
	}
	for class, character := range map[string]string{"Cc": "\u0007", "Cf": "\u200b", "Zl": "\u2028", "Zp": "\u2029"} {
		for _, key := range []string{"title", "repositoryName"} {
			envelope := envelopeMap(t, page)
			items, isList := envelope["evidence"].([]any)
			item, isObject := items[3].(map[string]any)
			if !isList || !isObject {
				t.Fatal("the envelope has no fourth evidence item")
			}
			item[key] = "octocat" + character + "hello"
			if broken := decodeProjectEnvelopeRules(t, envelope); len(broken) != 1 || broken[0] != "evidence "+key+" is 1 to 200 code points with no Cc, Cf, Zl or Zp character" {
				t.Errorf("%s in %s: broken = %q, want only the %s rule", class, key, broken, key)
			}
		}
	}
	envelope := envelopeMap(t, page)
	items, isList := envelope["evidence"].([]any)
	if !isList {
		t.Fatal("the envelope has no evidence array")
	}
	envelope["evidence"] = append(items, items[0])
	if broken := decodeProjectEnvelopeRules(t, envelope); len(broken) != 1 || broken[0] != "evidence is an array of at most 10 items" {
		t.Errorf("an eleventh item: broken = %q", broken)
	}
}
