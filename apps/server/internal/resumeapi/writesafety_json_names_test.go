package resumeapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestMutationRoutes_RejectInexactPropertyNames drives each JSON-body
// mutation route up to its decoder. A rejected name answers the route's
// request_invalid and names its path; exact names pass decoding and stop at
// the missing session, before any database work.
func TestMutationRoutes_RejectInexactPropertyNames(t *testing.T) {
	t.Parallel()

	resumeID := uuid.NewString()
	entryID := uuid.NewString()
	projectID := uuid.NewString()
	type route struct {
		name    string
		method  string
		match   bool
		section bool
		handler func(*Service, http.ResponseWriter, *http.Request)
		exact   string
		// rejected maps a body to the path its 400 message must name.
		rejected map[string]string
	}
	routes := []route{
		{
			name: "createResume", method: http.MethodPost, handler: (*Service).handleCreateResume,
			exact: `{"title":"x","lng":"en"}`,
			rejected: map[string]string{
				`{"Title":"x"}`:                          "Title",
				`{"title":"x","Title":"y"}`:              "Title",
				`{"title":"x","LNG":"en"}`:               "LNG",
				`{"title":"x","Document":{"a":"b"}}`:     "Document",
				`{"title":"x","document":{},"title2":1}`: "title2",
			},
		},
		{
			name: "updateResumeMetadata", method: http.MethodPatch, match: true, handler: (*Service).handleUpdateResumeMetadata,
			exact: `{"title":"x"}`,
			rejected: map[string]string{
				`{"Lng":"en"}`:              "Lng",
				`{"title":"x","Title":"y"}`: "Title",
			},
		},
		{
			name: "upsertResumeEntry", method: http.MethodPatch, match: true, section: true, handler: (*Service).handleUpsertResumeEntry,
			exact: `{"entry":{"id":"` + entryID + `"}}`,
			rejected: map[string]string{
				`{"Entry":{"id":"` + entryID + `"}}`:                                  "Entry",
				`{"entry":{"id":"` + entryID + `"},"ENTRY":{"id":"` + entryID + `"}}`: "ENTRY",
			},
		},
		{
			name: "updateResumeSection", method: http.MethodPatch, match: true, section: true, handler: (*Service).handleUpdateResumeSection,
			exact: `{"displayName":"x"}`,
			rejected: map[string]string{
				`{"DisplayName":"x"}`:                   "DisplayName",
				`{"displayName":"x","displayname":"y"}`: "displayname",
				`{"iconKey":null,"entryorder":["a"]}`:   "entryorder",
			},
		},
		{
			name: "updateResumeStructure", method: http.MethodPatch, match: true, handler: (*Service).handleUpdateResumeStructure,
			exact: `{"commands":[{"op":"deleteSection","key":"skills"}]}`,
			rejected: map[string]string{
				`{"Commands":[{"op":"deleteSection","key":"skills"}]}`:                                                           "Commands",
				`{"commands":[{"op":"deleteSection","key":"skills"},{"op":"deleteSection","Key":"x"}]}`:                          "commands[1].Key",
				`{"commands":[{"op":"moveSection","key":"a","column":"main","Index":0,"index":1}]}`:                              "commands[0].Index",
				`{"commands":[{"op":"deleteSection","key":"skills","KEY":"other"}]}`:                                             "commands[0].KEY",
				`{"commands":[{"Op":"deleteSection","key":"skills"}]}`:                                                           "commands[0].op",
				`{"commands":[{"op":"createSection","key":"a","sectionType":"custom","column":"main","index":0,"IconKey":"x"}]}`: "commands[0].IconKey",
			},
		},
		{
			name: "updateResumeCustomization", method: http.MethodPatch, match: true, handler: (*Service).handleUpdateResumeCustomization,
			exact: `{"deltas":[{"op":"set","path":"colors.primary","value":"#112233"}]}`,
			rejected: map[string]string{
				`{"Deltas":[{"op":"set","path":"colors.primary","value":"#112233"}]}`:                   "Deltas",
				`{"deltas":[{"op":"set","Path":"colors.primary","value":"#112233"}]}`:                   "deltas[0].Path",
				`{"deltas":[{"op":"set","path":"colors.primary","value":"#112233","VALUE":"#000000"}]}`: "deltas[0].VALUE",
			},
		},
		{
			name: "linkResumeProject", method: http.MethodPost, match: true, handler: (*Service).handleLinkResumeProject,
			exact: `{"projectId":"` + projectID + `"}`,
			rejected: map[string]string{
				`{"projectID":"` + projectID + `"}`:                                 "projectID",
				`{"projectId":"` + projectID + `","ProjectId":"` + projectID + `"}`: "ProjectId",
			},
		},
		{
			name: "linkResumeProject existing entry", method: http.MethodPost, match: true, handler: (*Service).handleLinkResumeProject,
			exact: `{"projectId":"` + projectID + `","entryId":"` + entryID + `"}`,
			rejected: map[string]string{
				`{"projectId":"` + projectID + `","EntryId":"` + entryID + `"}`:                             "EntryId",
				`{"projectId":"` + projectID + `","entryId":"` + entryID + `","entryID":"` + entryID + `"}`: "entryID",
			},
		},
	}
	send := func(t *testing.T, r route, body string) (int, errorObject) {
		t.Helper()
		req := httptest.NewRequestWithContext(context.Background(), r.method, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", uuid.NewString())
		if r.match {
			req.Header.Set("If-Match", `"r1"`)
		}
		req.SetPathValue("id", resumeID)
		if r.section {
			req.SetPathValue("sectionKey", "experience")
		}
		recorder := httptest.NewRecorder()
		r.handler(&Service{}, recorder, req)
		var envelope errorEnvelope
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
		}
		return recorder.Code, envelope.Error
	}
	for _, r := range routes {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			if status, failure := send(t, r, r.exact); status != http.StatusUnauthorized || failure.Code != "session_required" {
				t.Fatalf("exact names = %d %+v, want decoding to pass and stop at the session", status, failure)
			}
			for body, path := range r.rejected {
				status, failure := send(t, r, body)
				if status != http.StatusBadRequest || failure.Code != "request_invalid" {
					t.Errorf("%s = %d %+v, want 400 request_invalid", body, status, failure)
					continue
				}
				if !strings.Contains(failure.Message, `"`+path+`"`) && !strings.Contains(failure.Message, path+" ") {
					t.Errorf("%s message %q does not name %s", body, failure.Message, path)
				}
				if failure.Details != nil {
					t.Errorf("%s carries details %v; request_invalid has none", body, failure.Details)
				}
			}
		})
	}
}

// TestDecodeJSONBody_RejectsInexactNamesBeforeBinding checks the shared body
// decoder directly, including a struct nested below a slice.
func TestDecodeJSONBody_RejectsInexactNamesBeforeBinding(t *testing.T) {
	t.Parallel()

	for body, want := range map[string]string{
		`{"Deltas":[]}`: `request body field "Deltas" is not declared (the declared name is "deltas")`,
		`{"deltas":[{"op":"set","path":"a","value":1},{"OP":"unset","path":"a"}]}`: `request body field "deltas[1].OP" is not declared (the declared name is "op")`,
		`{"deltas":[{"op":"set","path":"a","Value":1,"value":2}]}`:                 `request body field "deltas[0].Value" is not declared (the declared name is "value")`,
		`{"deltas":[{"op":1}]}`:                             `request body field "deltas[0].op" must be a string`,
		`{"deltas":[],"deltas":[]}`:                         `request body field "deltas" is repeated`,
		`{"deltas":[{"op":"set","op":"unset","path":"a"}]}`: `request body field "deltas[0].op" is repeated`,
	} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPatch, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		var wire customizationRequestWire
		_, err := decodeJSONBody(req, &wire)
		if err == nil {
			t.Errorf("%s was accepted", body)
			continue
		}
		failure := mapMutationError(err)
		if failure.Code != "request_invalid" || failure.Message != want {
			t.Errorf("%s = %s %q, want request_invalid %q", body, failure.Code, failure.Message, want)
		}
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPatch, "/",
		strings.NewReader(`{"deltas":[{"op":"set","path":"a","value":{"Free":1}}]}`))
	req.Header.Set("Content-Type", "application/json")
	var wire customizationRequestWire
	if _, err := decodeJSONBody(req, &wire); err != nil {
		t.Fatalf("exact names with a raw value: %v", err)
	}
}
