package resumeapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

type boundsHTTPContract struct {
	requestBytes        int
	documentBytes       int
	sections            int
	entriesPerSection   int
	richTextBytes       int
	personalDetails     int
	titleCodePoints     int
	lngCodePoints       int
	structureCommands   int
	customizationDeltas int
}

func TestBoundsHTTPMatrixCompleteness(t *testing.T) {
	contract := loadBoundsHTTPContract(t)
	expected := map[string]int{
		"request_bytes":        contract.requestBytes,
		"document_bytes":       contract.documentBytes,
		"sections":             contract.sections,
		"entries_per_section":  contract.entriesPerSection,
		"rich_text_bytes":      contract.richTextBytes,
		"personal_details":     contract.personalDetails,
		"title_code_points":    contract.titleCodePoints,
		"lng_code_points":      contract.lngCodePoints,
		"structure_commands":   contract.structureCommands,
		"customization_deltas": contract.customizationDeltas,
	}
	exercised := make(map[string]int)
	for _, testCase := range boundsHTTPMatrixCases(contract) {
		if previous, ok := exercised[testCase.name]; ok {
			t.Fatalf("duplicate HTTP bound case %q with limits %d and %d", testCase.name, previous, testCase.limit)
		}
		exercised[testCase.name] = testCase.limit
	}

	var differences []string
	for name, want := range expected {
		got, ok := exercised[name]
		switch {
		case !ok:
			differences = append(differences, "missing HTTP bound case: "+name)
		case got != want:
			differences = append(differences, fmt.Sprintf("HTTP bound %s = %d, want %d", name, got, want))
		}
	}
	for name := range exercised {
		if _, ok := expected[name]; !ok {
			differences = append(differences, "undeclared HTTP bound case: "+name)
		}
	}
	if len(differences) > 0 {
		sort.Strings(differences)
		t.Fatalf("HTTP bound inventory mismatch:\n%s", strings.Join(differences, "\n"))
	}
}

func loadBoundsHTTPContract(t *testing.T) boundsHTTPContract {
	t.Helper()
	budgets, err := os.ReadFile("../../../../docs/plans/budgets.md")
	if err != nil {
		t.Fatalf("read bounds authority: %v", err)
	}
	released, err := schema.ReleasedSchemaFor(2)
	if err != nil {
		t.Fatalf("read released v2 schema: %v", err)
	}
	var frozen struct {
		Defs struct {
			RichText struct {
				MaxLength int `json:"maxLength"`
			} `json:"richText"`
			Content struct {
				MaxProperties int `json:"maxProperties"`
			} `json:"content"`
			Section struct {
				OneOf []struct {
					Properties struct {
						Entries struct {
							MaxItems int `json:"maxItems"`
						} `json:"entries"`
					} `json:"properties"`
				} `json:"oneOf"`
			} `json:"section"`
			PersonalDetails struct {
				Properties struct {
					Details struct {
						MaxItems int `json:"maxItems"`
					} `json:"details"`
				} `json:"properties"`
			} `json:"personalDetails"`
			Customization struct {
				Properties struct {
					Layout struct {
						Properties struct {
							Sections struct {
								Properties struct {
									Main struct {
										MaxItems int `json:"maxItems"`
									} `json:"main"`
									Sidebar struct {
										MaxItems int `json:"maxItems"`
									} `json:"sidebar"`
								} `json:"properties"`
							} `json:"sections"`
						} `json:"properties"`
					} `json:"layout"`
				} `json:"properties"`
			} `json:"customization"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(released.RawSchema, &frozen); err != nil {
		t.Fatalf("decode released v2 schema: %v", err)
	}
	if len(frozen.Defs.Section.OneOf) != 8 {
		t.Fatalf("released v2 section variants = %d, want 8", len(frozen.Defs.Section.OneOf))
	}
	entriesLimit := frozen.Defs.Section.OneOf[0].Properties.Entries.MaxItems
	for index, variant := range frozen.Defs.Section.OneOf {
		if variant.Properties.Entries.MaxItems != entriesLimit {
			t.Fatalf("released v2 section variant %d maxItems = %d, want shared %d", index, variant.Properties.Entries.MaxItems, entriesLimit)
		}
	}
	sectionsLimit := frozen.Defs.Content.MaxProperties
	for name, limit := range map[string]int{
		"layout.sections.main":    frozen.Defs.Customization.Properties.Layout.Properties.Sections.Properties.Main.MaxItems,
		"layout.sections.sidebar": frozen.Defs.Customization.Properties.Layout.Properties.Sections.Properties.Sidebar.MaxItems,
	} {
		if limit != sectionsLimit {
			t.Fatalf("released v2 %s maxItems = %d, want content maxProperties %d", name, limit, sectionsLimit)
		}
	}

	return boundsHTTPContract{
		requestBytes:        boundsBudget(t, budgets, "Request body", "KB") * 1024,
		documentBytes:       boundsBudget(t, budgets, "Resume document total", "KB") * 1024,
		sections:            sectionsLimit,
		entriesPerSection:   entriesLimit,
		richTextBytes:       frozen.Defs.RichText.MaxLength,
		personalDetails:     frozen.Defs.PersonalDetails.Properties.Details.MaxItems,
		titleCodePoints:     boundsBudget(t, budgets, "Resume title length", "characters"),
		lngCodePoints:       boundsBudget(t, budgets, "`lng` tag length", "characters"),
		structureCommands:   boundsBudget(t, budgets, "Structure commands per request", ""),
		customizationDeltas: boundsBudget(t, budgets, "Customization deltas per request", ""),
	}
}

func boundsBudget(t *testing.T, markdown []byte, name, unit string) int {
	t.Helper()
	for _, line := range strings.Split(string(markdown), "\n") {
		columns := strings.Split(line, "|")
		if len(columns) < 4 || strings.TrimSpace(columns[1]) != name {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(columns[2]))
		if len(fields) < 2 || fields[0] != "≤" {
			t.Fatalf("budget %q target = %q, want an upper bound", name, strings.TrimSpace(columns[2]))
		}
		value, err := strconv.Atoi(strings.ReplaceAll(fields[1], ",", ""))
		if err != nil {
			t.Fatalf("parse budget %q target %q: %v", name, strings.TrimSpace(columns[2]), err)
		}
		gotUnit := ""
		if len(fields) > 2 {
			gotUnit = fields[2]
		}
		if gotUnit != unit {
			t.Fatalf("budget %q unit = %q, want %q", name, gotUnit, unit)
		}
		return value
	}
	t.Fatalf("budget %q is missing", name)
	return 0
}

func TestRevisionSerializedAsString(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	const revision = int64(9_007_199_254_740_993)
	if _, err := h.pool.Exec(h.ctx, `UPDATE resumes SET revision = $1 WHERE id = $2`, revision, created.ID); err != nil {
		t.Fatalf("seed large revision: %v", err)
	}
	get := resumeRequest(t, h, http.MethodGet, apiResumePath+"/"+created.ID.String(), "", 0, uuid.Nil, "2")
	if get.status != http.StatusOK || responseRevision(t, get.body) != fmt.Sprint(revision) {
		t.Fatalf("GET large revision = %d %s", get.status, get.body)
	}
	list := resumeRequest(t, h, http.MethodGet, apiResumePath, "", 0, uuid.Nil, "2")
	if list.status != http.StatusOK || listResponseRevision(t, list.body) != fmt.Sprint(revision) {
		t.Fatalf("list large revision = %d %s", list.status, list.body)
	}
	stale := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+created.ID.String(), `{"title":"stale"}`, revision-1, uuid.New(), "2")
	if stale.status != http.StatusPreconditionFailed || errorResponseRevision(t, stale.body) != fmt.Sprint(revision) {
		t.Fatalf("412 large revision = %d %s", stale.status, stale.body)
	}
	written := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+created.ID.String(), `{"title":"exact"}`, revision, uuid.New(), "2")
	if written.status != http.StatusOK || responseRevision(t, written.body) != fmt.Sprint(revision+1) ||
		written.header.Get("ETag") != fmt.Sprintf(`"r%d"`, revision+1) {
		t.Fatalf("write large revision = %d headers=%v body=%s", written.status, written.header, written.body)
	}
}

type boundsHTTPCase struct {
	name  string
	limit int
	run   func(*testing.T, boundsHTTPContract)
}

type boundsHTTPWriteState struct {
	resumes            string
	idempotencyRecords string
	objects            []string
}

func TestByteVsCodePointBounds(t *testing.T) {
	contract := loadBoundsHTTPContract(t)
	t.Run("rich text counts UTF-8 bytes", func(t *testing.T) {
		h, created, path := newRichTextBoundsFixture(t)
		before := snapshotBoundsHTTPWriteState(t, h)
		body := richTextEntryBody(t, strings.Repeat("é", 9_000))
		if codePoints := 9_000; codePoints >= contract.richTextBytes || len(strings.Repeat("é", codePoints)) <= contract.richTextBytes {
			t.Fatalf("UTF-8 fixture does not distinguish code points from bytes")
		}
		response := resumeRequest(t, h, http.MethodPatch, path, body, created.Revision, uuid.New(), "2")
		assertRouteError(t, response, http.StatusUnprocessableEntity, "document_invalid")
		assertBoundsHTTPWriteState(t, h, before, "9,000-code-point rich text rejection")
	})

	t.Run("title counts code points", func(t *testing.T) {
		h := newResumeAPITestHarness(t)
		created := h.createResume(t)
		path := apiResumePath + "/" + created.ID.String()
		atLimit := strings.Repeat("😀", contract.titleCodePoints)
		accepted := resumeRequest(t, h, http.MethodPatch, path,
			string(mustResumeTestJSON(t, map[string]any{"title": atLimit})), created.Revision, uuid.New(), "2")
		if accepted.status != http.StatusOK {
			t.Fatalf("%d-code-point astral title = %d %s, want 200", contract.titleCodePoints, accepted.status, accepted.body)
		}
		before := snapshotBoundsHTTPWriteState(t, h)
		rejected := resumeRequest(t, h, http.MethodPatch, path,
			string(mustResumeTestJSON(t, map[string]any{"title": atLimit + "😀"})), created.Revision+1, uuid.New(), "2")
		assertRouteError(t, rejected, http.StatusUnprocessableEntity, "document_invalid")
		assertBoundsHTTPWriteState(t, h, before, "astral title limit+1 rejection")
	})
}

func TestEveryBound_LimitAndLimitPlusOne(t *testing.T) {
	runBoundsHTTPMatrix(t)
}

func TestRejectionWritesNothing(t *testing.T) {
	runBoundsHTTPMatrix(t)
}

func runBoundsHTTPMatrix(t *testing.T) {
	t.Helper()
	contract := loadBoundsHTTPContract(t)
	for _, testCase := range boundsHTTPMatrixCases(contract) {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.run(t, contract)
		})
	}
}

func boundsHTTPMatrixCases(contract boundsHTTPContract) []boundsHTTPCase {
	return []boundsHTTPCase{
		{"request_bytes", contract.requestBytes, testBoundsHTTPRequestBytes},
		{"document_bytes", contract.documentBytes, testBoundsHTTPDocumentBytes},
		{"sections", contract.sections, testBoundsHTTPSections},
		{"entries_per_section", contract.entriesPerSection, testBoundsHTTPEntries},
		{"rich_text_bytes", contract.richTextBytes, testBoundsHTTPRichText},
		{"personal_details", contract.personalDetails, testBoundsHTTPPersonalDetails},
		{"title_code_points", contract.titleCodePoints, testBoundsHTTPTitle},
		{"lng_code_points", contract.lngCodePoints, testBoundsHTTPLanguage},
		{"structure_commands", contract.structureCommands, testBoundsHTTPStructureCommands},
		{"customization_deltas", contract.customizationDeltas, testBoundsHTTPCustomizationDeltas},
	}
}

func testBoundsHTTPRequestBytes(t *testing.T, contract boundsHTTPContract) {
	h := newResumeAPITestHarness(t)
	prefix, suffix := `{"title":"request bound","document":`, `}`
	doc := exactCanonicalSizeDocument(t, contract.requestBytes-len(prefix)-len(suffix), contract.richTextBytes, contract.entriesPerSection)
	raw, err := resume.AssembleCanonical(doc)
	if err != nil {
		t.Fatalf("assemble request-bound fixture: %v", err)
	}
	body := prefix + string(raw) + suffix
	if len(body) != contract.requestBytes {
		t.Fatalf("request fixture = %d bytes, want %d", len(body), contract.requestBytes)
	}
	accepted := resumeRequest(t, h, http.MethodPost, apiResumePath, body, 0, uuid.New(), "2")
	if accepted.status != http.StatusCreated {
		t.Fatalf("request at %d bytes = %d %s, want 201", contract.requestBytes, accepted.status, accepted.body)
	}
	before := snapshotBoundsHTTPWriteState(t, h)
	rejected := resumeRequest(t, h, http.MethodPost, apiResumePath, body+" ", 0, uuid.New(), "2")
	assertRouteError(t, rejected, http.StatusRequestEntityTooLarge, "body_too_large")
	assertBoundsHTTPWriteState(t, h, before, "request-byte limit+1 rejection")
}

func testBoundsHTTPDocumentBytes(t *testing.T, contract boundsHTTPContract) {
	h := newResumeAPITestHarness(t)
	doc := exactCanonicalSizeDocument(t, contract.documentBytes, contract.richTextBytes, contract.entriesPerSection)
	created, err := h.resumes.Create(h.ctx, h.userID, "aggregate bound", doc)
	if err != nil {
		t.Fatalf("create exact aggregate fixture: %v", err)
	}
	path := apiResumePath + "/" + created.ID.String()
	accepted := resumeRequest(t, h, http.MethodPatch, path,
		`{"title":"aggregate bound accepted"}`, created.Revision, uuid.New(), "2")
	if accepted.status != http.StatusOK {
		t.Fatalf("document at %d bytes = %d %s, want 200", contract.documentBytes, accepted.status, accepted.body)
	}
	work := doc.Content["work"]
	last := work.WorkEntries[len(work.WorkEntries)-1]
	if last.Description == nil {
		t.Fatal("aggregate fixture last description is absent")
	}
	overEntry := last
	overDescription := *last.Description + "a"
	overEntry.Description = &overDescription
	body := string(mustResumeTestJSON(t, map[string]any{"entry": overEntry}))
	before := snapshotBoundsHTTPWriteState(t, h)
	rejected := resumeRequest(t, h, http.MethodPatch, path+"/entries/work", body, created.Revision+1, uuid.New(), "2")
	assertRouteError(t, rejected, http.StatusUnprocessableEntity, "document_invalid")
	assertBoundsHTTPWriteState(t, h, before, "document-byte limit+1 rejection")
}

func testBoundsHTTPSections(t *testing.T, contract boundsHTTPContract) {
	h := newResumeAPITestHarness(t)
	doc := loadMinimalDocument(t)
	doc.Content = make(map[string]schema.Section, contract.sections-1)
	doc.Customization.Layout.Sections.Main = make([]string, contract.sections-1)
	doc.Customization.Layout.Sections.Sidebar = []string{}
	for index := range contract.sections - 1 {
		key := fmt.Sprintf("%c", 'a'+index)
		doc.Content[key] = schema.NewWorkSection(nil, nil, []schema.WorkEntry{})
		doc.Customization.Layout.Sections.Main[index] = key
	}
	created, err := h.resumes.Create(h.ctx, h.userID, "section bound", doc)
	if err != nil {
		t.Fatalf("create section-bound fixture: %v", err)
	}
	path := apiResumePath + "/" + created.ID.String() + "/structure"
	atLimitBody := fmt.Sprintf(`{"commands":[{"op":"createSection","key":"x","sectionType":"work","column":"main","index":%d}]}`, contract.sections-1)
	accepted := resumeRequest(t, h, http.MethodPatch, path, atLimitBody, created.Revision, uuid.New(), "2")
	if accepted.status != http.StatusOK {
		t.Fatalf("section %d = %d %s, want 200", contract.sections, accepted.status, accepted.body)
	}
	before := snapshotBoundsHTTPWriteState(t, h)
	overBody := fmt.Sprintf(`{"commands":[{"op":"createSection","key":"y","sectionType":"work","column":"main","index":%d}]}`, contract.sections)
	rejected := resumeRequest(t, h, http.MethodPatch, path, overBody, created.Revision+1, uuid.New(), "2")
	assertRouteError(t, rejected, http.StatusUnprocessableEntity, "document_invalid")
	assertBoundsHTTPWriteState(t, h, before, "section limit+1 rejection")
}

func testBoundsHTTPEntries(t *testing.T, contract boundsHTTPContract) {
	h := newResumeAPITestHarness(t)
	doc := loadMinimalDocument(t)
	entries := make([]schema.WorkEntry, contract.entriesPerSection-1)
	for index := range entries {
		entries[index].ID = fmt.Sprintf("10000000-0000-4000-8000-%012d", index)
	}
	doc.Content = map[string]schema.Section{"work": schema.NewWorkSection(nil, nil, entries)}
	doc.Customization.Layout.Sections.Main = []string{"work"}
	doc.Customization.Layout.Sections.Sidebar = []string{}
	created, err := h.resumes.Create(h.ctx, h.userID, "entry bound", doc)
	if err != nil {
		t.Fatalf("create entry-bound fixture: %v", err)
	}
	path := apiResumePath + "/" + created.ID.String() + "/entries/work"
	accepted := resumeRequest(t, h, http.MethodPatch, path,
		`{"entry":{"id":"20000000-0000-4000-8000-000000000064"}}`, created.Revision, uuid.New(), "2")
	if accepted.status != http.StatusOK {
		t.Fatalf("entry %d = %d %s, want 200", contract.entriesPerSection, accepted.status, accepted.body)
	}
	before := snapshotBoundsHTTPWriteState(t, h)
	rejected := resumeRequest(t, h, http.MethodPatch, path,
		`{"entry":{"id":"20000000-0000-4000-8000-000000000065"}}`, created.Revision+1, uuid.New(), "2")
	assertRouteError(t, rejected, http.StatusUnprocessableEntity, "document_invalid")
	assertBoundsHTTPWriteState(t, h, before, "entry limit+1 rejection")
}

func testBoundsHTTPRichText(t *testing.T, contract boundsHTTPContract) {
	h, created, path := newRichTextBoundsFixture(t)
	atLimit := strings.Repeat("é", contract.richTextBytes/2)
	if len(atLimit) != contract.richTextBytes {
		t.Fatalf("rich-text fixture = %d bytes, want %d", len(atLimit), contract.richTextBytes)
	}
	accepted := resumeRequest(t, h, http.MethodPatch, path, richTextEntryBody(t, atLimit), created.Revision, uuid.New(), "2")
	if accepted.status != http.StatusOK {
		t.Fatalf("rich text at %d bytes = %d %s, want 200", contract.richTextBytes, accepted.status, accepted.body)
	}
	before := snapshotBoundsHTTPWriteState(t, h)
	rejected := resumeRequest(t, h, http.MethodPatch, path, richTextEntryBody(t, atLimit+"a"), created.Revision+1, uuid.New(), "2")
	assertRouteError(t, rejected, http.StatusUnprocessableEntity, "document_invalid")
	assertBoundsHTTPWriteState(t, h, before, "rich-text byte limit+1 rejection")
}

func testBoundsHTTPPersonalDetails(t *testing.T, contract boundsHTTPContract) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	path := apiResumePath + "/" + created.ID.String() + "/personal-details"
	details := make([]map[string]any, contract.personalDetails+1)
	for index := range details {
		details[index] = map[string]any{
			"id": fmt.Sprintf("30000000-0000-4000-8000-%012d", index), "type": "email", "value": "", "isHidden": false,
		}
	}
	bodyFor := func(count int) string {
		return string(mustResumeTestJSON(t, map[string]any{"details": details[:count]}))
	}
	accepted := resumeRequest(t, h, http.MethodPatch, path, bodyFor(contract.personalDetails), created.Revision, uuid.New(), "2")
	if accepted.status != http.StatusOK {
		t.Fatalf("%d details = %d %s, want 200", contract.personalDetails, accepted.status, accepted.body)
	}
	before := snapshotBoundsHTTPWriteState(t, h)
	rejected := resumeRequest(t, h, http.MethodPatch, path, bodyFor(contract.personalDetails+1), created.Revision+1, uuid.New(), "2")
	assertRouteError(t, rejected, http.StatusUnprocessableEntity, "document_invalid")
	assertBoundsHTTPWriteState(t, h, before, "personal-details limit+1 rejection")
}

func testBoundsHTTPTitle(t *testing.T, contract boundsHTTPContract) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	path := apiResumePath + "/" + created.ID.String()
	atLimit := strings.Repeat("😀", contract.titleCodePoints)
	accepted := resumeRequest(t, h, http.MethodPatch, path,
		string(mustResumeTestJSON(t, map[string]any{"title": atLimit})), created.Revision, uuid.New(), "2")
	if accepted.status != http.StatusOK {
		t.Fatalf("title at %d code points = %d %s, want 200", contract.titleCodePoints, accepted.status, accepted.body)
	}
	before := snapshotBoundsHTTPWriteState(t, h)
	rejected := resumeRequest(t, h, http.MethodPatch, path,
		string(mustResumeTestJSON(t, map[string]any{"title": atLimit + "😀"})), created.Revision+1, uuid.New(), "2")
	assertRouteError(t, rejected, http.StatusUnprocessableEntity, "document_invalid")
	assertBoundsHTTPWriteState(t, h, before, "title code-point limit+1 rejection")
}

func testBoundsHTTPLanguage(t *testing.T, contract boundsHTTPContract) {
	const atLimit = "en-x-12345678-12345678-12345678-abc"
	const limitPlusOne = "en-x-12345678-12345678-12345678-abcd"
	if len(atLimit) != contract.lngCodePoints || len(limitPlusOne) != contract.lngCodePoints+1 {
		t.Fatalf("language fixtures = %d/%d code points, want %d/%d", len(atLimit), len(limitPlusOne), contract.lngCodePoints, contract.lngCodePoints+1)
	}
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	path := apiResumePath + "/" + created.ID.String()
	accepted := resumeRequest(t, h, http.MethodPatch, path,
		string(mustResumeTestJSON(t, map[string]any{"lng": atLimit})), created.Revision, uuid.New(), "2")
	if accepted.status != http.StatusOK {
		t.Fatalf("lng at %d code points = %d %s, want 200", contract.lngCodePoints, accepted.status, accepted.body)
	}
	before := snapshotBoundsHTTPWriteState(t, h)
	rejected := resumeRequest(t, h, http.MethodPatch, path,
		string(mustResumeTestJSON(t, map[string]any{"lng": limitPlusOne})), created.Revision+1, uuid.New(), "2")
	assertRouteError(t, rejected, http.StatusUnprocessableEntity, "document_invalid")
	assertBoundsHTTPWriteState(t, h, before, "lng limit+1 rejection")
}

func testBoundsHTTPStructureCommands(t *testing.T, contract boundsHTTPContract) {
	h := newResumeAPITestHarness(t)
	doc := loadMinimalDocument(t)
	doc.Content = map[string]schema.Section{"work": schema.NewWorkSection(nil, nil, []schema.WorkEntry{})}
	doc.Customization.Layout.Sections.Main = []string{"work"}
	doc.Customization.Layout.Sections.Sidebar = []string{}
	created, err := h.resumes.Create(h.ctx, h.userID, "structure command bound", doc)
	if err != nil {
		t.Fatalf("create structure-command fixture: %v", err)
	}
	path := apiResumePath + "/" + created.ID.String() + "/structure"
	command := `{"op":"reorderColumn","column":"main","keys":["work"]}`
	accepted := resumeRequest(t, h, http.MethodPatch, path,
		`{"commands":`+repeatedJSONArray(command, contract.structureCommands)+`}`,
		created.Revision, uuid.New(), "2")
	if accepted.status != http.StatusOK {
		t.Fatalf("%d structure commands = %d %s, want 200", contract.structureCommands, accepted.status, accepted.body)
	}
	before := snapshotBoundsHTTPWriteState(t, h)
	rejected := resumeRequest(t, h, http.MethodPatch, path,
		`{"commands":`+repeatedJSONArray(command, contract.structureCommands+1)+`}`,
		created.Revision+1, uuid.New(), "2")
	assertRouteError(t, rejected, http.StatusUnprocessableEntity, "document_invalid")
	assertBoundsHTTPWriteState(t, h, before, "structure-command limit+1 rejection")
}

func testBoundsHTTPCustomizationDeltas(t *testing.T, contract boundsHTTPContract) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	path := apiResumePath + "/" + created.ID.String() + "/customization"
	delta := `{"op":"set","path":"font.baseSizePx","value":16}`
	accepted := resumeRequest(t, h, http.MethodPatch, path,
		`{"deltas":`+repeatedJSONArray(delta, contract.customizationDeltas)+`}`,
		created.Revision, uuid.New(), "2")
	if accepted.status != http.StatusOK {
		t.Fatalf("%d customization deltas = %d %s, want 200", contract.customizationDeltas, accepted.status, accepted.body)
	}
	before := snapshotBoundsHTTPWriteState(t, h)
	rejected := resumeRequest(t, h, http.MethodPatch, path,
		`{"deltas":`+repeatedJSONArray(delta, contract.customizationDeltas+1)+`}`,
		created.Revision+1, uuid.New(), "2")
	assertRouteError(t, rejected, http.StatusUnprocessableEntity, "document_invalid")
	assertBoundsHTTPWriteState(t, h, before, "customization-delta limit+1 rejection")
}

func TestNoUnboundedWork(t *testing.T) {
	contract := loadBoundsHTTPContract(t)
	h := newResumeAPITestHarness(t)
	doc := loadMinimalDocument(t)
	doc.Content = map[string]schema.Section{"work": schema.NewWorkSection(nil, nil, []schema.WorkEntry{})}
	doc.Customization.Layout.Sections.Main = []string{"work"}
	doc.Customization.Layout.Sections.Sidebar = []string{}
	created, err := h.resumes.Create(h.ctx, h.userID, "unbounded work", doc)
	if err != nil {
		t.Fatalf("create unbounded-work fixture: %v", err)
	}

	validDelta := `{"op":"set","path":"font.baseSizePx","value":16}`
	validCommand := `{"op":"reorderColumn","column":"main","keys":["work"]}`
	requests := []struct {
		name, method, path, body string
		revision                 int64
	}{
		{"101 customization deltas", http.MethodPatch, apiResumePath + "/" + created.ID.String() + "/customization", `{"deltas":` + repeatedJSONArray(validDelta, contract.customizationDeltas+1) + `}`, created.Revision},
		{"10,000 customization deltas", http.MethodPatch, apiResumePath + "/" + created.ID.String() + "/customization", `{"deltas":` + repeatedJSONArray(`{}`, 10_000) + `}`, created.Revision},
		{"101 structure commands", http.MethodPatch, apiResumePath + "/" + created.ID.String() + "/structure", `{"commands":` + repeatedJSONArray(validCommand, contract.structureCommands+1) + `}`, created.Revision},
		{"10,000 structure commands", http.MethodPatch, apiResumePath + "/" + created.ID.String() + "/structure", `{"commands":` + repeatedJSONArray(`{}`, 10_000) + `}`, created.Revision},
	}

	oversizeEntries := make([]schema.WorkEntry, 10_000)
	entryArrayDoc := loadMinimalDocument(t)
	entryArrayDoc.Content = map[string]schema.Section{"work": schema.NewWorkSection(nil, nil, oversizeEntries)}
	entryArrayDoc.Customization.Layout.Sections.Main = []string{"work"}
	entryArrayDoc.Customization.Layout.Sections.Sidebar = []string{}
	entryArrayBody := string(mustResumeTestJSON(t, map[string]any{"title": "declared entry array bound", "document": entryArrayDoc}))
	if len(entryArrayBody) >= contract.requestBytes {
		t.Fatalf("10,000-entry fixture = %d bytes, must stay below transport bound %d", len(entryArrayBody), contract.requestBytes)
	}
	requests = append(requests, struct {
		name, method, path, body string
		revision                 int64
	}{"10,000 section entries", http.MethodPost, apiResumePath, entryArrayBody, 0})

	before := snapshotBoundsHTTPWriteState(t, h)
	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			response := resumeRequest(t, h, request.method, request.path, request.body, request.revision, uuid.New(), "2")
			assertRouteError(t, response, http.StatusUnprocessableEntity, "document_invalid")
			assertBoundsHTTPWriteState(t, h, before, request.name+" rejection")
		})
	}
}

func newRichTextBoundsFixture(t *testing.T) (*resumeAPITestHarness, resume.Resume, string) {
	t.Helper()
	h := newResumeAPITestHarness(t)
	doc := loadMinimalDocument(t)
	doc.Content = map[string]schema.Section{"profile": schema.NewProfileSection(nil, nil, []schema.ProfileEntry{})}
	doc.Customization.Layout.Sections.Main = []string{"profile"}
	doc.Customization.Layout.Sections.Sidebar = []string{}
	created, err := h.resumes.Create(h.ctx, h.userID, "UTF-8 bound", doc)
	if err != nil {
		t.Fatalf("create UTF-8 fixture: %v", err)
	}
	return h, created, apiResumePath + "/" + created.ID.String() + "/entries/profile"
}

func richTextEntryBody(t *testing.T, value string) string {
	t.Helper()
	return string(mustResumeTestJSON(t, map[string]any{"entry": map[string]any{
		"id": "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", "text": value,
	}}))
}

func repeatedJSONArray(member string, count int) string {
	if count <= 0 {
		return "[]"
	}
	return "[" + strings.TrimSuffix(strings.Repeat(member+",", count), ",") + "]"
}

func snapshotBoundsHTTPWriteState(t *testing.T, h *resumeAPITestHarness) boundsHTTPWriteState {
	t.Helper()
	return boundsHTTPWriteState{
		resumes:            h.snapshotUserTable(t, "resumes"),
		idempotencyRecords: h.snapshotUserTable(t, "idempotency_records"),
		objects:            snapshotObjectKeys(t, h),
	}
}

func assertBoundsHTTPWriteState(t *testing.T, h *resumeAPITestHarness, want boundsHTTPWriteState, context string) {
	t.Helper()
	if got := snapshotBoundsHTTPWriteState(t, h); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s wrote state:\n got=%+v\nwant=%+v", context, got, want)
	}
}

func responseRevision(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Data struct {
			Revision string `json:"revision"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode revision response: %v", err)
	}
	return envelope.Data.Revision
}

func listResponseRevision(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Data []struct {
			Revision string `json:"revision"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Data) != 1 {
		t.Fatalf("decode revision list: len=%d err=%v", len(envelope.Data), err)
	}
	return envelope.Data[0].Revision
}

func errorResponseRevision(t *testing.T, body []byte) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Details struct {
				Revision string `json:"revision"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode revision error: %v", err)
	}
	return envelope.Error.Details.Revision
}

func exactCanonicalSizeDocument(t *testing.T, target, richTextLimit, entriesLimit int) schema.Resume {
	t.Helper()
	doc := loadMinimalDocument(t)
	entries := make([]schema.WorkEntry, 0, entriesLimit)
	for len(entries) < cap(entries) {
		text := ""
		entries = append(entries, schema.WorkEntry{
			ID:          fmt.Sprintf("00000000-0000-4000-8000-%012x", len(entries)+1),
			Description: &text,
		})
		doc.Content = map[string]schema.Section{"work": schema.NewWorkSection(nil, nil, entries)}
		doc.Customization.Layout.Sections.Main = []string{"work"}
		doc.Customization.Layout.Sections.Sidebar = []string{}
		raw, err := resume.AssembleCanonical(doc)
		if err != nil {
			t.Fatalf("assemble aggregate with %d entries: %v", len(entries), err)
		}
		padding := target - len(raw)
		if padding < 0 {
			t.Fatalf("aggregate base with %d entries = %d bytes, exceeds target %d", len(entries), len(raw), target)
		}
		if padding <= richTextLimit {
			text = strings.Repeat("a", padding)
			entries[len(entries)-1].Description = &text
			doc.Content["work"] = schema.NewWorkSection(nil, nil, entries)
			raw, err = resume.AssembleCanonical(doc)
			if err != nil || len(raw) != target {
				t.Fatalf("aggregate size = %d err=%v, want %d", len(raw), err, target)
			}
			return doc
		}
		text = strings.Repeat("a", richTextLimit)
		entries[len(entries)-1].Description = &text
	}
	t.Fatalf("cannot build %d-byte aggregate within the released entry and rich-text bounds", target)
	return schema.Resume{}
}
