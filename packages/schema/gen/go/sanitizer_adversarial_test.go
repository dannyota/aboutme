package schema

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

const (
	allowlistSourcePath = "../../validation/sanitizer-allowlist.v1.json"
	corpusSourcePath    = "../../validation/hostile-corpus.json"
	resumeSchemaPath    = "../../resume.schema.json"
)

type sanitizerAllowlistSource struct {
	Version          int                 `json:"version"`
	Tags             []string            `json:"tags"`
	Attributes       map[string][]string `json:"attributes"`
	GlobalAttributes []string            `json:"globalAttributes"`
	URLSchemes       []string            `json:"urlSchemes"`
	Forbidden        struct {
		Tags              []string `json:"tags"`
		AttributePrefixes []string `json:"attributePrefixes"`
		URLSchemes        []string `json:"urlSchemes"`
	} `json:"forbidden"`
	LinkHardening struct {
		ExternalRel string `json:"externalRel"`
	} `json:"linkHardening"`
}

type hostileCorpusSource struct {
	Version  int `json:"version"`
	Payloads []struct {
		ID       string `json:"id"`
		Category string `json:"category"`
		Payload  string `json:"payload"`
	} `json:"payloads"`
}

type resumeSchemaSource struct {
	Defs struct {
		SanitizerAllowlistVersion struct {
			Const int `json:"const"`
		} `json:"sanitizerAllowlistVersion"`
	} `json:"$defs"`
}

func TestGeneratedSanitizerPolicyMatchesJSONSources(t *testing.T) {
	t.Parallel()

	allowlist := readSanitizerJSON[sanitizerAllowlistSource](t, allowlistSourcePath)
	corpus := readSanitizerJSON[hostileCorpusSource](t, corpusSourcePath)
	schemaSource := readSanitizerJSON[resumeSchemaSource](t, resumeSchemaPath)

	if SanitizerAllowlistVersion != allowlist.Version {
		t.Errorf("SanitizerAllowlistVersion = %d, allowlist version = %d", SanitizerAllowlistVersion, allowlist.Version)
	}
	if SanitizerAllowlistVersion != corpus.Version {
		t.Errorf("SanitizerAllowlistVersion = %d, corpus version = %d", SanitizerAllowlistVersion, corpus.Version)
	}
	if SanitizerAllowlistVersion != schemaSource.Defs.SanitizerAllowlistVersion.Const {
		t.Errorf(
			"SanitizerAllowlistVersion = %d, resume schema version = %d",
			SanitizerAllowlistVersion,
			schemaSource.Defs.SanitizerAllowlistVersion.Const,
		)
	}

	assertSanitizerValue(t, "AllowedTags", AllowedTags, allowlist.Tags)
	assertSanitizerValue(t, "AllowedAttributes", AllowedAttributes, allowlist.Attributes)
	assertSanitizerValue(t, "AllowedURLSchemes", AllowedURLSchemes, allowlist.URLSchemes)
	assertSanitizerValue(t, "ForbiddenTags", ForbiddenTags, allowlist.Forbidden.Tags)
	assertSanitizerValue(t, "ForbiddenAttributePrefixes", ForbiddenAttributePrefixes, allowlist.Forbidden.AttributePrefixes)
	assertSanitizerValue(t, "ForbiddenURLSchemes", ForbiddenURLSchemes, allowlist.Forbidden.URLSchemes)

	if len(allowlist.GlobalAttributes) != 0 {
		t.Fatalf("globalAttributes has %d entries, but the generated Go interface has no global-attribute field", len(allowlist.GlobalAttributes))
	}
	if ExternalRel != allowlist.LinkHardening.ExternalRel {
		t.Errorf("ExternalRel = %q, want %q", ExternalRel, allowlist.LinkHardening.ExternalRel)
	}

	if len(HostileCorpus) != len(corpus.Payloads) {
		t.Fatalf("HostileCorpus has %d rows, want %d", len(HostileCorpus), len(corpus.Payloads))
	}
	seenIDs := make(map[string]struct{}, len(corpus.Payloads))
	for index, sourceRow := range corpus.Payloads {
		if sourceRow.ID == "" {
			t.Fatalf("source hostile corpus row %d has an empty id", index)
		}
		if _, duplicate := seenIDs[sourceRow.ID]; duplicate {
			t.Fatalf("source hostile corpus id %q is duplicated", sourceRow.ID)
		}
		seenIDs[sourceRow.ID] = struct{}{}

		generatedRow := HostileCorpus[index]
		if generatedRow.ID != sourceRow.ID {
			t.Errorf("HostileCorpus[%d].ID = %q, want %q", index, generatedRow.ID, sourceRow.ID)
		}
		if generatedRow.Category != sourceRow.Category {
			t.Errorf("HostileCorpus[%d] (%q).Category = %q, want %q", index, sourceRow.ID, generatedRow.Category, sourceRow.Category)
		}
		if generatedRow.Payload != sourceRow.Payload {
			t.Errorf("HostileCorpus[%d] (%q).Payload = %q, want %q", index, sourceRow.ID, generatedRow.Payload, sourceRow.Payload)
		}
	}
}

func TestGeneratedSanitizerCollectionsAreIndependentFromDecodedSources(t *testing.T) {
	allowlist := readSanitizerJSON[sanitizerAllowlistSource](t, allowlistSourcePath)
	corpus := readSanitizerJSON[hostileCorpusSource](t, corpusSourcePath)

	generatedTags := append([]string(nil), AllowedTags...)
	generatedAttributes := cloneSanitizerStringMap(AllowedAttributes)
	generatedCorpus := append([]HostilePayload(nil), HostileCorpus...)

	allowlist.Tags[0] = "source-mutation"
	allowlist.Attributes["a"][0] = "source-mutation"
	corpus.Payloads[0].Payload = "source-mutation"

	assertSanitizerValue(t, "AllowedTags after source mutation", AllowedTags, generatedTags)
	assertSanitizerValue(t, "AllowedAttributes after source mutation", AllowedAttributes, generatedAttributes)
	assertSanitizerValue(t, "HostileCorpus after source mutation", HostileCorpus, generatedCorpus)
}

func readSanitizerJSON[T any](t *testing.T, path string) T {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return result
}

func assertSanitizerValue(t *testing.T, name string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", name, got, want)
	}
}

func cloneSanitizerStringMap(source map[string][]string) map[string][]string {
	clone := make(map[string][]string, len(source))
	for key, values := range source {
		clone[key] = append([]string(nil), values...)
	}
	return clone
}
