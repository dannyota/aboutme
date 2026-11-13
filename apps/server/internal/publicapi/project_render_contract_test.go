package publicapi

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"danny.vn/aboutme/apps/server/internal/directrender"
)

// TestProjectRenderCorpusMatchesEnvelopeTypes proves every envelope in the
// shared project render corpus decodes into the Go request type of its mode
// with no unknown field and encodes back to the same JSON, so Go and the
// renderer read one contract (docs/design/projects-contract.md "Render
// envelope").
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
