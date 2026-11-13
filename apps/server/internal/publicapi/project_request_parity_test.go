package publicapi

import (
	"strings"
	"testing"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

var combiningAccent = string(rune(0x301))

func TestProjectDescription_OutcomeCutsAtCodePoints(t *testing.T) {
	for name, test := range map[string]struct{ outcome, want string }{
		"accent across the cut": {strings.Repeat("a", 159) + "e" + combiningAccent + "bbbb", strings.Repeat("a", 159) + "e"},
		"cut on a boundary":     {strings.Repeat("a", 160) + "e" + combiningAccent, strings.Repeat("a", 160)},
		"exactly 160":           {strings.Repeat("a", 158) + "e" + combiningAccent, strings.Repeat("a", 158) + "e" + combiningAccent},
		"precomposed letter":    {strings.Repeat("a", 159) + "é" + "b", strings.Repeat("a", 159) + "é"},
		"wide characters":       {wideText(200), wideText(160)},
	} {
		if got := projectDescription(project.Text{Outcome: test.outcome}); got != test.want {
			t.Errorf("%s: description = %q (%d code points), want %q", name, got, len([]rune(got)), test.want)
		}
	}
}

func TestProjectRequests_LeaveOutBlankOptionalText(t *testing.T) {
	blank := " \u00a0" + string(rune(0xfeff))
	text := project.Text{Name: "Engine", Role: blank, Summary: blank, Outcome: "Outcome.", Description: blank}
	input := projectTestInput(t, projectTestSnapshot("en", "Ada", nil, schema.MmYyyy, false), 1,
		publicresume.PublicProject{Slug: "engine", Document: project.Document{DefaultLanguage: "en", Text: map[string]project.Text{"en": text}}})
	page, err := buildProjectRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	if page.Project.Role != "" || page.Project.Summary != "" || page.Project.Description != "" || page.Preview.Description != "Outcome." {
		t.Errorf("page project = %+v, preview %q; want blank texts left out and the outcome as description", page.Project, page.Preview.Description)
	}
	list, err := buildProjectListRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	if row := list.Projects[0]; row.Role != "" || row.Summary != "" {
		t.Errorf("list row = %+v, want blank texts left out", row)
	}
	text.Outcome = blank
	input.pages.Projects[0].Document.Text["en"] = text
	page, err = buildProjectRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	if page.Project.Outcome != "" || page.Preview.Description != "" {
		t.Errorf("blank outcome = %q, preview %q; want both empty", page.Project.Outcome, page.Preview.Description)
	}
	if broken := decodeProjectEnvelopeRules(t, page); len(broken) != 0 {
		t.Errorf("decoder copy refuses the page: %q", broken)
	}
}

func TestProjectDecoderRules_FollowTheRendererAt7ed24b18(t *testing.T) {
	outcome := strings.Repeat("a", 159) + "e" + combiningAccent + "bbbb"
	stored := limitProject("parity", 2, nil)
	text := stored.Document.Text["en"]
	text.Summary, text.Outcome = "", outcome
	stored.Document.Text["en"] = text
	stored.EntryLink = "https://ada.example/notes"
	input := projectTestInput(t, projectTestSnapshot("en", "Ada", nil, schema.MmYyyy, false), 1, stored)
	page, err := buildProjectRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	list, err := buildProjectListRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	if broken := decodeProjectEnvelopeRules(t, page); len(broken) != 0 {
		t.Fatalf("baseline page breaks the decoder copy: %q", broken)
	}
	field := func(envelope map[string]any, key string) map[string]any {
		object, isObject := envelope[key].(map[string]any)
		if !isObject {
			t.Fatal(key)
		}
		return object
	}
	for name, mutate := range map[string]func(map[string]any){
		"description cut at a grapheme": func(e map[string]any) { field(e, "preview")["description"] = strings.Repeat("a", 159) },
		"whitespace summary beside the outcome": func(e map[string]any) {
			field(e, "project")["summary"] = "   "
		},
		"NUL in the name": func(e map[string]any) { field(e, "project")["name"] = "Par\x00ity" },
		"entry link equal by host": func(e map[string]any) {
			e["entryLink"] = strings.Replace(stored.Document.RepositoryURL, "ada.example", "ADA.example", 1)
		},
		"dates ending before start": func(e map[string]any) {
			field(e, "project")["dates"] = map[string]any{"start": map[string]any{"y": 2021.0}, "end": map[string]any{"y": 2020.0}, "present": false}
		},
		"userinfo Go refuses": func(e map[string]any) { field(e, "project")["demoUrl"] = "https://us^er@ada.example/demo" },
	} {
		envelope := envelopeMap(t, page)
		mutate(envelope)
		if broken := decodeProjectEnvelopeRules(t, envelope); len(broken) == 0 {
			t.Errorf("page %s: the copy accepted it; the renderer refuses it", name)
		}
	}
	listEnvelope := envelopeMap(t, list)
	field(listEnvelope, "preview")["description"] = "x"
	if broken := decodeProjectEnvelopeRules(t, listEnvelope); len(broken) == 0 {
		t.Error("list with a description: the copy accepted it; the renderer refuses it")
	}
}
