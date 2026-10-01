package resumeapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

func TestDecodePublishShowcaseFields(t *testing.T) {
	t.Parallel()

	present, err := decodePublish(strings.NewReader(`{` + publishFlags + `,"showcaseEnabled":true,"showcaseRole":"backend"}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !present.ShowcaseEnabled.Present || !present.ShowcaseEnabled.Value || !present.ShowcaseRole.Present || present.ShowcaseRole.Value != "backend" {
		t.Fatalf("decoded = %+v", present)
	}
	cleared, err := decodePublish(strings.NewReader(`{` + publishFlags + `,"showcaseEnabled":false,"showcaseRole":""}`))
	if err != nil || !cleared.ShowcaseEnabled.Present || cleared.ShowcaseEnabled.Value || !cleared.ShowcaseRole.Present || cleared.ShowcaseRole.Value != "" {
		t.Fatalf("cleared = %+v err=%v", cleared, err)
	}
	absent, err := decodePublish(strings.NewReader(`{` + publishFlags + `}`))
	if err != nil || absent.ShowcaseEnabled.Present || absent.ShowcaseRole.Present {
		t.Fatalf("absent fields decoded as present: %+v %v", absent, err)
	}
	for field, value := range map[string]string{
		"showcaseEnabled": "null", "showcaseRole": "null",
	} {
		_, decodeErr := decodePublish(strings.NewReader(`{` + publishFlags + `,"` + field + `":` + value + `}`))
		var shape *publishShapeError
		if !errors.As(decodeErr, &shape) || shape.Field != field {
			t.Fatalf("%s=%s error = %#v, want shape error", field, value, decodeErr)
		}
	}
	for field, value := range map[string]string{"showcaseEnabled": `"true"`, "showcaseRole": `7`} {
		_, decodeErr := decodePublish(strings.NewReader(`{` + publishFlags + `,"` + field + `":` + value + `}`))
		var shape *publishShapeError
		if !errors.As(decodeErr, &shape) || shape.Field != field {
			t.Fatalf("%s=%s error = %#v, want shape error", field, value, decodeErr)
		}
	}
}

// AC-SHOW-001, AC-SHOW-003: the switch is on only while the resume is live with
// sign in to view off, omitted fields keep the stored state, and a stored
// opt-in ends when the request leaves the resume not live or turns sign in to
// view on.
func TestResolveShowcase(t *testing.T) {
	t.Parallel()
	backend, qa := "backend", "qa"
	liveOpen := currentPublish{Live: true}
	liveGated := currentPublish{Live: true, SignInToView: true}
	notLive := currentPublish{}
	listed := currentPublish{Live: true, ShowcaseEnabled: true, ShowcaseRole: &backend}

	tests := []struct {
		name        string
		current     currentPublish
		effective   currentPublish
		input       publishInput
		wantEnabled bool
		wantRole    *string
		wantIssues  []string
	}{
		{name: "off and omitted stays off", current: currentPublish{Live: true}, effective: liveOpen},
		{name: "omitted keeps the opt-in and its role", current: listed, effective: liveOpen, wantEnabled: true, wantRole: &backend},
		{name: "turning on", current: currentPublish{Live: true}, effective: liveOpen,
			input: publishInput{ShowcaseEnabled: optionalBool{Present: true, Value: true}}, wantEnabled: true},
		{name: "turning on with a role", current: currentPublish{Live: true}, effective: liveOpen,
			input:       publishInput{ShowcaseEnabled: optionalBool{Present: true, Value: true}, ShowcaseRole: optionalText{Present: true, Value: "qa"}},
			wantEnabled: true, wantRole: &qa},
		{name: "turning on needs live", current: notLive, effective: notLive,
			input:      publishInput{ShowcaseEnabled: optionalBool{Present: true, Value: true}},
			wantIssues: []string{"showcaseEnabled:requires_live"}},
		{name: "turning on needs sign in to view off", current: liveOpen, effective: liveGated,
			input:      publishInput{ShowcaseEnabled: optionalBool{Present: true, Value: true}},
			wantIssues: []string{"showcaseEnabled:requires_open_view"}},
		{name: "turning on needs both", current: notLive, effective: currentPublish{SignInToView: true},
			input:      publishInput{ShowcaseEnabled: optionalBool{Present: true, Value: true}},
			wantIssues: []string{"showcaseEnabled:requires_live", "showcaseEnabled:requires_open_view"}},
		{name: "turning off clears the role", current: listed, effective: liveOpen,
			input: publishInput{ShowcaseEnabled: optionalBool{Present: true, Value: false}}},
		{name: "unpublish ends an omitted opt-in", current: listed, effective: notLive},
		{name: "sign in to view ends an omitted opt-in", current: listed, effective: liveGated},
		{name: "role changes while on", current: listed, effective: liveOpen,
			input:       publishInput{ShowcaseRole: optionalText{Present: true, Value: "qa"}},
			wantEnabled: true, wantRole: &qa},
		{name: "empty role clears it", current: listed, effective: liveOpen,
			input:       publishInput{ShowcaseRole: optionalText{Present: true, Value: ""}},
			wantEnabled: true},
		{name: "role outside the list", current: listed, effective: liveOpen,
			input:       publishInput{ShowcaseRole: optionalText{Present: true, Value: "designer"}},
			wantEnabled: true, wantIssues: []string{"showcaseRole:invalid_format"}},
		{name: "role needs the switch on", current: currentPublish{Live: true}, effective: liveOpen,
			input:      publishInput{ShowcaseRole: optionalText{Present: true, Value: "backend"}},
			wantIssues: []string{"showcaseRole:invalid_format"}},
		{name: "role with the switch turned off", current: listed, effective: liveOpen,
			input: publishInput{
				ShowcaseEnabled: optionalBool{Present: true, Value: false},
				ShowcaseRole:    optionalText{Present: true, Value: "backend"},
			},
			wantIssues: []string{"showcaseRole:invalid_format"}},
		{name: "empty role with the switch off is fine", current: currentPublish{Live: true}, effective: liveOpen,
			input: publishInput{ShowcaseRole: optionalText{Present: true, Value: ""}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			enabled, role, issues := resolveShowcase(test.current, test.effective, test.input)
			if enabled != test.wantEnabled {
				t.Errorf("enabled = %t, want %t", enabled, test.wantEnabled)
			}
			if (role == nil) != (test.wantRole == nil) || (role != nil && *role != *test.wantRole) {
				t.Errorf("role = %v, want %v", role, test.wantRole)
			}
			got := make([]string, 0, len(issues))
			for _, issue := range issues {
				got = append(got, issue.Path+":"+issue.Code)
			}
			slices.Sort(got)
			if !slices.Equal(got, test.wantIssues) && (len(got) != 0 || len(test.wantIssues) != 0) {
				t.Errorf("issues = %v, want %v", got, test.wantIssues)
			}
		})
	}
}

// AC-SHOW-001: validatePublish carries the resolved state into the effective
// publish settings.
func TestValidatePublishCarriesShowcase(t *testing.T) {
	t.Parallel()
	role := "devops"
	current := currentPublish{Live: true, ShowcaseEnabled: true, ShowcaseRole: &role}
	kept := validatePublish(publishCompleteDocumentForUnit(), current, publishInput{Live: true})
	if !kept.Effective.ShowcaseEnabled || kept.Effective.ShowcaseRole == nil || *kept.Effective.ShowcaseRole != "devops" {
		t.Fatalf("effective = %+v, want the opt-in kept", kept.Effective)
	}
	ended := validatePublish(publishCompleteDocumentForUnit(), current, publishInput{Live: false})
	if ended.Effective.ShowcaseEnabled || ended.Effective.ShowcaseRole != nil {
		t.Fatalf("effective = %+v, want the opt-in ended by unpublish", ended.Effective)
	}
}

func publishCompleteDocumentForUnit() schema.Resume {
	name := "Ada Lovelace"
	return schema.Resume{PersonalDetails: schema.PersonalDetails{FullName: &name}}
}

type showcaseResumeBody struct {
	Revision string
	Showcase map[string]any
	Raw      map[string]json.RawMessage
}

func decodeShowcaseBody(t *testing.T, body []byte) showcaseResumeBody {
	t.Helper()
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Data == nil {
		t.Fatalf("decode resource: %v (%s)", err, body)
	}
	out := showcaseResumeBody{Raw: envelope.Data}
	if err := json.Unmarshal(envelope.Data["revision"], &out.Revision); err != nil {
		t.Fatalf("decode revision: %v (%s)", err, body)
	}
	raw, present := envelope.Data["showcase"]
	if !present {
		t.Fatalf("resource lacks showcase: %s", body)
	}
	if err := json.Unmarshal(raw, &out.Showcase); err != nil {
		t.Fatalf("decode showcase: %v (%s)", err, body)
	}
	return out
}

func (b showcaseResumeBody) revision(t *testing.T) int64 {
	t.Helper()
	revision, err := strconv.ParseInt(b.Revision, 10, 64)
	if err != nil {
		t.Fatalf("revision %q: %v", b.Revision, err)
	}
	return revision
}

func (b showcaseResumeBody) requireState(t *testing.T, wantState, wantRole string) {
	t.Helper()
	if wantState == "" {
		if b.Showcase != nil {
			t.Fatalf("showcase = %v, want null", b.Showcase)
		}
		return
	}
	if b.Showcase == nil || b.Showcase["state"] != wantState {
		t.Fatalf("showcase = %v, want state %q", b.Showcase, wantState)
	}
	role, hasRole := b.Showcase["role"]
	switch {
	case wantRole == "" && role != nil:
		t.Fatalf("showcase role = %v, want null", role)
	case wantRole != "" && (!hasRole || role != wantRole):
		t.Fatalf("showcase role = %v, want %q", role, wantRole)
	}
	if len(b.Showcase) != 2 {
		t.Fatalf("showcase = %v, want exactly state and role", b.Showcase)
	}
}

type showcasePublishEnv struct {
	h        *resumeAPITestHarness
	id       uuid.UUID
	slug     string
	path     string
	revision int64
}

func newShowcasePublishEnv(t *testing.T) *showcasePublishEnv {
	t.Helper()
	h := newResumeAPITestHarness(t)
	h.service.signInToViewEnabled = true
	created, err := h.resumes.Create(h.ctx, h.userID, "Showcase", publishCompleteDocument(t))
	if err != nil {
		t.Fatalf("create publishable resume: %v", err)
	}
	return &showcasePublishEnv{
		h: h, id: created.ID, slug: "show-" + uuid.NewString()[:8],
		path: apiResumePath + "/" + created.ID.String() + "/publish", revision: created.Revision,
	}
}

// publish sends one publish body and, on success, tracks the new revision.
func (e *showcasePublishEnv) publish(t *testing.T, body string) testHTTPResponse {
	t.Helper()
	response := e.h.mutationRequest(t, http.MethodPost, e.path, strings.NewReader(body), e.revision, uuid.NewString())
	if response.status == http.StatusOK {
		e.revision = decodeShowcaseBody(t, response.body).revision(t)
	}
	return response
}

func (e *showcasePublishEnv) publishOK(t *testing.T, body string) showcaseResumeBody {
	t.Helper()
	response := e.publish(t, body)
	if response.status != http.StatusOK {
		t.Fatalf("publish %s = %d %s", body, response.status, response.body)
	}
	return decodeShowcaseBody(t, response.body)
}

func (e *showcasePublishEnv) row(t *testing.T) (found bool, requestedAt time.Time) {
	t.Helper()
	row, err := e.h.queries.GetResumeShowcase(e.h.ctx, e.id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, time.Time{}
	}
	if err != nil {
		t.Fatalf("read showcase row: %v", err)
	}
	return true, row.RequestedAt
}

func publishBody(slug string, fields string) string {
	body := `{"slug":"` + slug + `","live":true,"downloadEnabled":true,"seoGeoEnabled":false`
	if fields != "" {
		body += "," + fields
	}
	return body + "}"
}

// AC-SHOW-001, AC-SHOW-003, AC-SHOW-005: opting in lists at once, omitted
// fields keep the state, and every ending path deletes the row, so publishing
// again and opting in again start from nothing.
func TestPublishShowcaseLifecycle(t *testing.T) {
	env := newShowcasePublishEnv(t)

	listed := env.publishOK(t, publishBody(env.slug, `"showcaseEnabled":true,"showcaseRole":"backend"`))
	listed.requireState(t, "listed", "backend")
	exists, firstRequested := env.row(t)
	if !exists {
		t.Fatal("no showcase row after the opt-in")
	}
	stored, err := env.h.resumes.Get(env.h.ctx, env.h.userID, env.id)
	if err != nil || stored.Showcase == nil || stored.Showcase.State != "listed" {
		t.Fatalf("stored showcase = %+v err=%v, want listed", stored.Showcase, err)
	}

	env.publishOK(t, `{"live":true,"downloadEnabled":false,"seoGeoEnabled":false}`).requireState(t, "listed", "backend")
	env.publishOK(t, `{"live":true,"downloadEnabled":false,"seoGeoEnabled":false,"showcaseRole":"qa"}`).requireState(t, "listed", "qa")
	env.publishOK(t, `{"live":true,"downloadEnabled":false,"seoGeoEnabled":false,"showcaseRole":""}`).requireState(t, "listed", "")
	if found, requested := env.row(t); !found || !requested.Equal(firstRequested) {
		t.Fatalf("after role changes found %t requested %v, want the first opt-in time %v", found, requested, firstRequested)
	}

	// Turning sign in to view on ends the opt-in although the field is omitted.
	env.publishOK(t, `{"live":true,"downloadEnabled":false,"seoGeoEnabled":false,"signInToView":true}`).requireState(t, "", "")
	if found, _ := env.row(t); found {
		t.Fatal("sign in to view left the showcase row")
	}
	// Turning it off again does not bring the opt-in back.
	env.publishOK(t, `{"live":true,"downloadEnabled":false,"seoGeoEnabled":false,"signInToView":false}`).requireState(t, "", "")

	// Opt in, opt out, opt in: the second request is a new opt-in.
	env.publishOK(t, `{"live":true,"downloadEnabled":false,"seoGeoEnabled":false,"showcaseEnabled":true}`).requireState(t, "listed", "")
	env.publishOK(t, `{"live":true,"downloadEnabled":false,"seoGeoEnabled":false,"showcaseEnabled":false}`).requireState(t, "", "")
	if found, _ := env.row(t); found {
		t.Fatal("opt-out left the showcase row")
	}
	env.publishOK(t, `{"live":true,"downloadEnabled":false,"seoGeoEnabled":false,"showcaseEnabled":true}`).requireState(t, "listed", "")
	if found, requested := env.row(t); !found || requested.Before(firstRequested) {
		t.Fatalf("second opt-in = found %t requested %v, want a new row no older than %v", found, requested, firstRequested)
	}

	// Unpublishing ends the opt-in, and publishing again starts with it off.
	env.publishOK(t, `{"live":false,"downloadEnabled":false,"seoGeoEnabled":false}`).requireState(t, "", "")
	if found, _ := env.row(t); found {
		t.Fatal("unpublish left the showcase row")
	}
	env.publishOK(t, `{"live":true,"downloadEnabled":false,"seoGeoEnabled":false}`).requireState(t, "", "")
	if found, _ := env.row(t); found {
		t.Fatal("republish created a showcase row")
	}
}

// AC-SHOW-001: the owner resource and list carry showcase, null until opted in.
func TestOwnerResumeCarriesShowcase(t *testing.T) {
	env := newShowcasePublishEnv(t)
	got := env.h.request(t, http.MethodGet, apiResumePath+"/"+env.id.String(), nil, true, false)
	if got.status != http.StatusOK {
		t.Fatalf("get = %d %s", got.status, got.body)
	}
	decodeShowcaseBody(t, got.body).requireState(t, "", "")

	env.publishOK(t, publishBody(env.slug, `"showcaseEnabled":true,"showcaseRole":"frontend"`))
	got = env.h.request(t, http.MethodGet, apiResumePath+"/"+env.id.String(), nil, true, false)
	decodeShowcaseBody(t, got.body).requireState(t, "listed", "frontend")

	list := env.h.request(t, http.MethodGet, apiResumePath, nil, true, false)
	var envelope struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(list.body, &envelope); err != nil || len(envelope.Data) != 1 {
		t.Fatalf("decode list: %v (%s)", err, list.body)
	}
	var listed map[string]any
	if err := json.Unmarshal(envelope.Data[0]["showcase"], &listed); err != nil || listed["state"] != "listed" || listed["role"] != "frontend" {
		t.Fatalf("listed showcase = %v err=%v", listed, err)
	}
}

type publishIssuesBody struct {
	Error struct {
		Code    string `json:"code"`
		Details struct {
			Issues []struct {
				Path string `json:"path"`
				Code string `json:"code"`
			} `json:"issues"`
		} `json:"details"`
	} `json:"error"`
}

func publishIssueList(t *testing.T, response testHTTPResponse) []string {
	t.Helper()
	assertResumeTestError(t, response, http.StatusUnprocessableEntity, "publish_invalid")
	var body publishIssuesBody
	if err := json.Unmarshal(response.body, &body); err != nil {
		t.Fatalf("decode issues: %v (%s)", err, response.body)
	}
	issues := make([]string, 0, len(body.Error.Details.Issues))
	for _, issue := range body.Error.Details.Issues {
		issues = append(issues, issue.Path+":"+issue.Code)
	}
	slices.Sort(issues)
	return issues
}

// AC-SHOW-001, AC-SHOW-003: the closed publish issues for the showcase fields,
// and a rejected request leaves no row.
func TestPublishShowcaseIssues(t *testing.T) {
	env := newShowcasePublishEnv(t)

	notLive := env.publish(t, `{"slug":"`+env.slug+`","live":false,"downloadEnabled":true,"seoGeoEnabled":false,"showcaseEnabled":true}`)
	if got := publishIssueList(t, notLive); !slices.Equal(got, []string{"showcaseEnabled:requires_live"}) {
		t.Fatalf("not live issues = %v", got)
	}
	gated := env.publish(t, publishBody(env.slug, `"signInToView":true,"showcaseEnabled":true`))
	if got := publishIssueList(t, gated); !slices.Equal(got, []string{"showcaseEnabled:requires_open_view"}) {
		t.Fatalf("sign in to view issues = %v", got)
	}
	badRole := env.publish(t, publishBody(env.slug, `"showcaseEnabled":true,"showcaseRole":"designer"`))
	if got := publishIssueList(t, badRole); !slices.Equal(got, []string{"showcaseRole:invalid_format"}) {
		t.Fatalf("bad role issues = %v", got)
	}
	roleWithoutSwitch := env.publish(t, publishBody(env.slug, `"showcaseRole":"backend"`))
	if got := publishIssueList(t, roleWithoutSwitch); !slices.Equal(got, []string{"showcaseRole:invalid_format"}) {
		t.Fatalf("role without switch issues = %v", got)
	}
	for _, body := range []string{
		publishBody(env.slug, `"showcaseEnabled":null`),
		publishBody(env.slug, `"showcaseRole":null`),
		publishBody(env.slug, `"showcaseEnabled":"yes"`),
	} {
		assertResumeTestError(t, env.publish(t, body), http.StatusBadRequest, "request_invalid")
	}
	if found, _ := env.row(t); found {
		t.Fatal("a rejected publish left a showcase row")
	}
	stored, err := env.h.resumes.Get(env.h.ctx, env.h.userID, env.id)
	if err != nil || stored.Live || stored.Showcase != nil {
		t.Fatalf("stored = live %t showcase %+v err=%v, want the resume untouched", stored.Live, stored.Showcase, err)
	}
}

// AC-SHOW-006: an idempotent replay returns the stored response and does not
// run the publish again, so a change made since stays.
func TestPublishShowcaseIdempotentReplay(t *testing.T) {
	env := newShowcasePublishEnv(t)
	key := uuid.NewString()
	body := publishBody(env.slug, `"showcaseEnabled":true,"showcaseRole":"devops"`)
	first := env.h.mutationRequest(t, http.MethodPost, env.path, strings.NewReader(body), env.revision, key)
	if first.status != http.StatusOK {
		t.Fatalf("publish = %d %s", first.status, first.body)
	}
	decodeShowcaseBody(t, first.body).requireState(t, "listed", "devops")

	if _, err := env.h.pool.Exec(env.h.ctx, `UPDATE resume_showcase SET role = 'qa' WHERE resume_id = $1`, env.id); err != nil {
		t.Fatalf("change role: %v", err)
	}
	replay := env.h.mutationRequest(t, http.MethodPost, env.path, strings.NewReader(body), env.revision, key)
	if replay.status != first.status || string(replay.body) != string(first.body) {
		t.Fatalf("replay = %d %s, want the first response %d %s", replay.status, replay.body, first.status, first.body)
	}
	row, err := env.h.queries.GetResumeShowcase(env.h.ctx, env.id)
	if err != nil || row.Role == nil || *row.Role != "qa" {
		t.Fatalf("after replay row = %+v err=%v, want the later role kept", row, err)
	}
}

// AC-SHOW-006: a document write to an opted-in resume recomputes the card
// version in the same transaction, and the resume stays listed with its opt-in
// time.
func TestDocumentWritesRecomputeShowcase(t *testing.T) {
	env := newShowcasePublishEnv(t)
	env.publishOK(t, publishBody(env.slug, `"showcaseEnabled":true`))
	h := env.h

	readRow := func() (cardVersion string, requestedAt time.Time, state string) {
		t.Helper()
		row, err := h.queries.GetResumeShowcase(h.ctx, env.id)
		if err != nil {
			t.Fatalf("read showcase row: %v", err)
		}
		resumed, err := h.resumes.Get(h.ctx, h.userID, env.id)
		if err != nil || resumed.Showcase == nil {
			t.Fatalf("get resume = %+v err=%v", resumed.Showcase, err)
		}
		return row.CardVersion, row.RequestedAt, resumed.Showcase.State
	}
	patch := func(suffix, body string) {
		t.Helper()
		current, err := h.resumes.Get(h.ctx, h.userID, env.id)
		if err != nil {
			t.Fatalf("get resume: %v", err)
		}
		response := h.mutationRequest(t, http.MethodPatch, apiResumePath+"/"+env.id.String()+suffix, strings.NewReader(body), current.Revision, uuid.NewString())
		if response.status != http.StatusOK {
			t.Fatalf("patch %s = %d %s", suffix, response.status, response.body)
		}
	}

	version0, requested0, state := readRow()
	if state != "listed" {
		t.Fatalf("state = %q, want listed", state)
	}

	patch("/customization", `{"deltas":[{"op":"set","path":"colors.accent","value":"#336699"}]}`)
	version1, requested1, state := readRow()
	if version1 == version0 || state != "listed" || !requested1.Equal(requested0) {
		t.Fatalf("after color change version %s->%s state %s requested %v->%v, want a new version, listed, same opt-in time", version0, version1, state, requested0, requested1)
	}

	patch("/personal-details", `{"fullName":"Grace Hopper"}`)
	version2, requested2, state := readRow()
	if version2 == version1 || state != "listed" || !requested2.Equal(requested0) {
		t.Fatalf("after name change version %s->%s state %s, want a new version and listed", version1, version2, state)
	}

	current, err := h.resumes.Get(h.ctx, h.userID, env.id)
	if err != nil {
		t.Fatalf("get resume: %v", err)
	}
	upload := h.uploadPhotoRequest(t, env.id, current.Revision, uuid.NewString(), "photo.png", makePhotoPNG(t))
	if upload.status != http.StatusOK {
		t.Fatalf("photo upload = %d %s", upload.status, upload.body)
	}
	version3, _, state := readRow()
	if version3 == version2 || state != "listed" {
		t.Fatalf("after photo version %s->%s state %s, want a new version and listed", version2, version3, state)
	}
}

// AC-SHOW-006: a write to a resume without a row touches no showcase row.
func TestDocumentWriteWithoutOptInTouchesNoShowcaseRow(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	response := h.mutationRequest(t, http.MethodPatch, apiResumePath+"/"+created.ID.String()+"/personal-details",
		strings.NewReader(`{"fullName":"No Opt In"}`), created.Revision, uuid.NewString())
	if response.status != http.StatusOK {
		t.Fatalf("patch = %d %s", response.status, response.body)
	}
	var rows int
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM resume_showcase WHERE resume_id = $1`, created.ID).Scan(&rows); err != nil {
		t.Fatalf("count showcase rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("showcase rows = %d, want 0", rows)
	}
}
