package publicapi

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/project"
)

func TestProjectRenderCorpusMatchesEnvelopeTypes(t *testing.T) {
	raw, err := os.ReadFile("testdata/project-render.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Name     string          `json:"name"`
			Envelope json.RawMessage `json:"envelope"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	modes := map[string]bool{}
	for _, tc := range corpus.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			var head struct {
				Mode string `json:"mode"`
			}
			if err := json.Unmarshal(tc.Envelope, &head); err != nil {
				t.Fatal(err)
			}
			var envelope any
			switch head.Mode {
			case directrender.ProjectListRenderMode:
				envelope = &directrender.ProjectListRenderRequest{}
			case directrender.ProjectRenderMode:
				envelope = &directrender.ProjectRenderRequest{}
			default:
				t.Fatalf("mode = %q, want a project mode", head.Mode)
			}
			modes[head.Mode] = true
			decoder := json.NewDecoder(bytes.NewReader(tc.Envelope))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(envelope); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			remarshaled, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			if err := json.Unmarshal(remarshaled, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(tc.Envelope, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("envelope does not round-trip:\n got %s\nwant %s", remarshaled, tc.Envelope)
			}
		})
	}
	if !modes[directrender.ProjectListRenderMode] || !modes[directrender.ProjectRenderMode] {
		t.Fatalf("corpus modes = %v, want both project modes", modes)
	}
}

func TestProjectRenderCorpusHoldsStorableProjects(t *testing.T) {
	raw, err := os.ReadFile("testdata/project-render.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Name     string          `json:"name"`
			Envelope json.RawMessage `json:"envelope"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	for _, tc := range corpus.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			var head struct {
				Mode string `json:"mode"`
			}
			if err := json.Unmarshal(tc.Envelope, &head); err != nil {
				t.Fatal(err)
			}
			switch head.Mode {
			case directrender.ProjectListRenderMode:
				var envelope directrender.ProjectListRenderRequest
				if err := json.Unmarshal(tc.Envelope, &envelope); err != nil {
					t.Fatal(err)
				}
				for _, row := range envelope.Projects {
					if len(row.Technologies) > 6 || row.MoreTechnologies < 0 || (row.MoreTechnologies > 0 && len(row.Technologies) != 6) {
						t.Fatalf("row %s shows %d technologies and counts %d more", row.Slug, len(row.Technologies), row.MoreTechnologies)
					}
					requireStorable(t, envelope.Resume, project.Text{Name: row.Name, Role: row.Role, Summary: row.Summary}, row.Lang, project.Document{Dates: row.Dates, Technologies: row.Technologies})
				}
			case directrender.ProjectRenderMode:
				var envelope directrender.ProjectRenderRequest
				if err := json.Unmarshal(tc.Envelope, &envelope); err != nil {
					t.Fatal(err)
				}
				detail := envelope.Project
				requireStorable(t, envelope.Resume, project.Text{Name: detail.Name, Role: detail.Role, Summary: detail.Summary, Outcome: detail.Outcome, Description: detail.Description}, detail.Lang,
					project.Document{Dates: detail.Dates, RepositoryURL: detail.RepositoryURL, DemoURL: detail.DemoURL, Technologies: detail.Technologies})
				if envelope.EntryLink != "" {
					if normalized, ok := project.NormalizeLink(envelope.EntryLink); !ok || normalized != envelope.EntryLink {
						t.Fatalf("entryLink %q is not a normalized https link", envelope.EntryLink)
					}
				}
			default:
				t.Fatalf("mode = %q, want a project mode", head.Mode)
			}
		})
	}
}

func requireStorable(t *testing.T, resume directrender.ProjectPageResume, text project.Text, lang string, fields project.Document) {
	t.Helper()
	if lang == "" {
		lang, _, _ = strings.Cut(strings.ToLower(resume.Lng), "-")
	}
	fields.DefaultLanguage = lang
	fields.Text = map[string]project.Text{lang: text}
	prepared, err := project.Prepare(fields)
	if err != nil {
		t.Fatalf("project %q cannot be stored: %v", text.Name, err)
	}
	if !reflect.DeepEqual(prepared.Dates, fields.Dates) || prepared.RepositoryURL != fields.RepositoryURL || prepared.DemoURL != fields.DemoURL ||
		!slices.Equal(prepared.Technologies, fields.Technologies) || prepared.Text[lang] != text {
		t.Fatalf("project %q changes when stored:\n got %+v\nwant %+v", text.Name, prepared, fields)
	}
}
