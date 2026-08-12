package schema

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestGeneratedSanitizerDataMatchesSources(t *testing.T) {
	t.Parallel()

	var allowlist struct {
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
	readGeneratedSanitizerSource(t, "../../validation/sanitizer-allowlist.v1.json", &allowlist)

	var corpus struct {
		Version  int `json:"version"`
		Payloads []struct {
			ID       string `json:"id"`
			Category string `json:"category"`
			Payload  string `json:"payload"`
		} `json:"payloads"`
	}
	readGeneratedSanitizerSource(t, "../../validation/hostile-corpus.json", &corpus)

	var resumeSchema struct {
		Defs struct {
			SanitizerAllowlistVersion struct {
				Const int `json:"const"`
			} `json:"sanitizerAllowlistVersion"`
		} `json:"$defs"`
	}
	readGeneratedSanitizerSource(t, "../../resume.schema.json", &resumeSchema)

	if SanitizerAllowlistVersion != allowlist.Version || SanitizerAllowlistVersion != corpus.Version || SanitizerAllowlistVersion != resumeSchema.Defs.SanitizerAllowlistVersion.Const {
		t.Fatalf("generated version = %d, allowlist = %d, corpus = %d, schema = %d", SanitizerAllowlistVersion, allowlist.Version, corpus.Version, resumeSchema.Defs.SanitizerAllowlistVersion.Const)
	}
	if len(allowlist.GlobalAttributes) != 0 {
		t.Fatalf("global attributes are not supported by the generated contract: %v", allowlist.GlobalAttributes)
	}
	assertGeneratedSanitizerEqual(t, "tags", AllowedTags, allowlist.Tags)
	assertGeneratedSanitizerEqual(t, "attributes", AllowedAttributes, allowlist.Attributes)
	assertGeneratedSanitizerEqual(t, "URL schemes", AllowedURLSchemes, allowlist.URLSchemes)
	assertGeneratedSanitizerEqual(t, "forbidden tags", ForbiddenTags, allowlist.Forbidden.Tags)
	assertGeneratedSanitizerEqual(t, "forbidden attribute prefixes", ForbiddenAttributePrefixes, allowlist.Forbidden.AttributePrefixes)
	assertGeneratedSanitizerEqual(t, "forbidden URL schemes", ForbiddenURLSchemes, allowlist.Forbidden.URLSchemes)
	if ExternalRel != allowlist.LinkHardening.ExternalRel {
		t.Errorf("ExternalRel = %q, want %q", ExternalRel, allowlist.LinkHardening.ExternalRel)
	}
	if len(HostileCorpus) != len(corpus.Payloads) {
		t.Fatalf("HostileCorpus has %d rows, want %d", len(HostileCorpus), len(corpus.Payloads))
	}
	for index, source := range corpus.Payloads {
		want := HostilePayload{ID: source.ID, Category: source.Category, Payload: source.Payload}
		if HostileCorpus[index] != want {
			t.Errorf("HostileCorpus[%d] = %#v, want %#v", index, HostileCorpus[index], want)
		}
	}
}

func readGeneratedSanitizerSource(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func assertGeneratedSanitizerEqual(t *testing.T, name string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", name, got, want)
	}
}
