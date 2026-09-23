package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Every fake in this package is bounded: a fixed call budget, fixed-size
// account, and tiny images. A fake never loops on its own.
const fakeCallBudget = 80

var (
	errFakeBudget   = errors.New("fake call budget exhausted")
	errFakeLost     = errors.New("fake response lost")
	errFakeRejected = errors.New("fake unauthorized")
	fakeEpoch       = time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)
)

const (
	fakeSourceID     = "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f"
	fakeOtherID      = "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60"
	fakeAccessToken  = "access-token-value"
	fakeRefreshToken = "refresh-token-value"
)

type fakeResume struct {
	summary  resumeSummary
	document []byte
	photo    *photoBytes
}

type fakeIdempotent struct {
	payloadDigest string
	response      mutationResult
}

// fakeMCP is an in-memory account behind the sdkToolClient surface, plus the
// server observations and revocation transport the runtime would provide.
type fakeMCP struct {
	t *testing.T

	mu          sync.Mutex
	calls       []string
	arguments   []map[string]any
	now         time.Time
	order       []string
	resumes     map[string]*fakeResume
	idempotency map[string]fakeIdempotent
	nextTarget  int

	revoked       bool
	revokeCalls   int
	probeCalls    int
	refreshCalls  int
	sentinelProbe func()
	seq           uint64
	authSeq       uint64
	authDate      time.Time
	deniedSeq     uint64
	deniedDate    time.Time
	refusedSeq    uint64

	// Hooks for adversarial cases.
	dropCreate      bool
	loseCreate      bool
	rejectCreate    string
	uploadConflict  bool
	uploadLose      bool
	changeSourceOn  int
	sourceGets      int
	refuseReauth    bool
	ignoreRevoke    bool
	noTokenDate     bool
	probeStatus     int
	probeDateOffset time.Duration
}

func newFakeMCP(t *testing.T, withPhoto bool) *fakeMCP {
	t.Helper()
	f := &fakeMCP{t: t, now: fakeEpoch, resumes: map[string]*fakeResume{}, idempotency: map[string]fakeIdempotent{}, refuseReauth: true}
	document := fixtureDocument(t, withPhoto)
	source := &fakeResume{summary: fakeSummary(fakeSourceID, sourceLanguage, "English CV", "3"), document: document}
	if withPhoto {
		source.photo = &photoBytes{ContentType: "image/jpeg", Data: testJPEG(t, 8, 8)}
	}
	f.resumes[fakeSourceID] = source
	f.order = append(f.order, fakeSourceID)
	return f
}

func fakeSummary(id, lng, title, revision string) resumeSummary {
	return resumeSummary{ID: id, Title: title, Lng: lng, Revision: revision, SchemaVersion: 4, CreatedAt: fakeEpoch.Add(-time.Hour), UpdatedAt: fakeEpoch.Add(-time.Hour)}
}

func (f *fakeMCP) addOther(lng string) {
	f.resumes[fakeOtherID] = &fakeResume{summary: fakeSummary(fakeOtherID, lng, "Other CV", "1"), document: fixtureDocument(f.t, false)}
	f.order = append(f.order, fakeOtherID)
}

func (f *fakeMCP) ListTools(context.Context, *mcp.ListToolsParams) (*mcp.ListToolsResult, error) {
	tools := make([]*mcp.Tool, 0, len(requiredTools))
	for _, name := range requiredTools {
		tools = append(tools, &mcp.Tool{Name: name})
	}
	return &mcp.ListToolsResult{Tools: tools}, nil
}

func (*fakeMCP) Close() error { return nil }

func (f *fakeMCP) CallTool(_ context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) >= fakeCallBudget {
		return nil, errFakeBudget
	}
	arguments, ok := params.Arguments.(map[string]any)
	if !ok {
		return nil, errFakeBudget
	}
	f.calls = append(f.calls, params.Name)
	f.arguments = append(f.arguments, arguments)
	f.now = f.now.Add(time.Second)
	f.seq++
	if f.revoked {
		f.deniedSeq, f.deniedDate = f.seq, f.now
		if f.refuseReauth {
			f.seq++
			f.refusedSeq = f.seq
		}
		return nil, errFakeRejected
	}
	f.authSeq, f.authDate = f.seq, f.now
	switch params.Name {
	case "list_resumes":
		listing := make([]resumeSummary, 0, len(f.order))
		for _, id := range f.order {
			listing = append(listing, f.resumes[id].summary)
		}
		return fakeResult(listedResumes{Resumes: listing}), nil
	case "get_resume":
		return f.getResume(arguments)
	case "get_photo":
		item := f.resumes[fmt.Sprint(arguments["resume_id"])]
		if item == nil || item.photo == nil {
			return fakeError("not_found"), nil
		}
		return fakeResult(getPhotoResult{ContentType: item.photo.ContentType, DataBase64: base64.StdEncoding.EncodeToString(item.photo.Data)}), nil
	case "create_resume":
		return f.create(arguments)
	case "upload_photo":
		return f.upload(arguments)
	case "update_photo_crop":
		return f.crop(arguments)
	default:
		f.t.Errorf("runner called forbidden tool %q", params.Name)
		return fakeError("validation_failed"), nil
	}
}

func (f *fakeMCP) getResume(arguments map[string]any) (*mcp.CallToolResult, error) {
	id := fmt.Sprint(arguments["resume_id"])
	item := f.resumes[id]
	if item == nil {
		return fakeError("not_found"), nil
	}
	if id == fakeSourceID {
		f.sourceGets++
		if f.changeSourceOn > 0 && f.sourceGets == f.changeSourceOn {
			item.summary.Revision = bumpRevision(item.summary.Revision)
		}
	}
	return fakeResult(getResumeResult{State: resumeState{resumeSummary: item.summary, Document: item.document}}), nil
}

func (f *fakeMCP) create(arguments map[string]any) (*mcp.CallToolResult, error) {
	key := fmt.Sprint(arguments["idempotency_key"])
	payload, encoded := fakeDocumentBytes(arguments["document"])
	if !encoded {
		return fakeError("validation_failed"), nil
	}
	if stored, ok := f.idempotency[key]; ok {
		if stored.payloadDigest != digest(payload) {
			return fakeError("validation_failed"), nil
		}
		return fakeResult(stored.response), nil
	}
	if f.rejectCreate != "" {
		return fakeError(f.rejectCreate), nil
	}
	if f.dropCreate {
		return nil, errFakeLost
	}
	f.nextTarget++
	id := fmt.Sprintf("01890f47-7e8a-7b2a-8d70-%012d", 100+f.nextTarget)
	summary := fakeSummary(id, fmt.Sprint(arguments["lng"]), fmt.Sprint(arguments["title"]), "1")
	summary.CreatedAt, summary.UpdatedAt = f.now, f.now
	f.resumes[id] = &fakeResume{summary: summary, document: payload}
	f.order = append(f.order, id)
	response := mutationResult{Revision: "1", State: resumeState{resumeSummary: summary, Document: payload}}
	f.idempotency[key] = fakeIdempotent{payloadDigest: digest(payload), response: response}
	if f.loseCreate {
		return nil, errFakeLost
	}
	return fakeResult(response), nil
}

func (f *fakeMCP) upload(arguments map[string]any) (*mcp.CallToolResult, error) {
	item := f.resumes[fmt.Sprint(arguments["resume_id"])]
	if item == nil || fmt.Sprint(arguments["resume_id"]) == fakeSourceID {
		f.t.Errorf("upload named a non-target resume")
		return fakeError("not_found"), nil
	}
	if f.uploadConflict || fmt.Sprint(arguments["revision"]) != item.summary.Revision {
		return fakeError("revision_conflict"), nil
	}
	data, decoded := fakePhotoBytes(arguments["data_base64"])
	if !decoded {
		return fakeError("validation_failed"), nil
	}
	item.photo = &photoBytes{ContentType: "image/jpeg", Data: data}
	item.document = withDocumentPhoto(f.t, item.document, &schema.Photo{Key: "resumes/target/photo.jpg"})
	item.summary.Revision = bumpRevision(item.summary.Revision)
	if f.uploadLose {
		return nil, errFakeLost
	}
	return fakeResult(mutationResult{Revision: item.summary.Revision, State: resumeState{resumeSummary: item.summary, Document: item.document}}), nil
}

func (f *fakeMCP) crop(arguments map[string]any) (*mcp.CallToolResult, error) {
	item := f.resumes[fmt.Sprint(arguments["resume_id"])]
	if item == nil || fmt.Sprint(arguments["resume_id"]) == fakeSourceID {
		f.t.Errorf("crop named a non-target resume")
		return fakeError("not_found"), nil
	}
	if fmt.Sprint(arguments["revision"]) != item.summary.Revision {
		return fakeError("revision_conflict"), nil
	}
	raw, ok := arguments["crop"].(map[string]any)
	if !ok || item.photo == nil {
		return fakeError("validation_failed"), nil
	}
	number := func(key string) float64 {
		value, isNumber := raw[key].(float64)
		if !isNumber {
			return -1
		}
		return value
	}
	item.document = withDocumentPhoto(f.t, item.document, &schema.Photo{Key: "resumes/target/photo.jpg", Crop: &schema.PhotoCrop{X: number("x"), Y: number("y"), Width: number("width"), Height: number("height")}})
	item.summary.Revision = bumpRevision(item.summary.Revision)
	return fakeResult(mutationResult{Revision: item.summary.Revision, State: resumeState{resumeSummary: item.summary, Document: item.document}}), nil
}

// serverObservations for the fake.
func (f *fakeMCP) mark() uint64 { f.mu.Lock(); defer f.mu.Unlock(); return f.seq }

func (f *fakeMCP) authenticatedDateSince(mark uint64) (time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.authDate, f.authSeq > mark
}

func (f *fakeMCP) tokenDates() (time.Time, time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.noTokenDate {
		return time.Time{}, time.Time{}, false
	}
	return fakeEpoch.Add(-10 * time.Second), fakeEpoch.Add(-10 * time.Second), true
}

func (f *fakeMCP) deniedSince(mark uint64, accessDigest string) (time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deniedDate, f.deniedSeq > mark && accessDigest == digest([]byte(fakeAccessToken))
}

func (f *fakeMCP) reauthorizationRefusedSince(mark uint64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refusedSeq > mark
}

// revocationTransport for the fake.
func (f *fakeMCP) Revoke(_ context.Context, refreshToken string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sentinelProbe != nil {
		f.sentinelProbe()
	}
	f.revokeCalls++
	if refreshToken == fakeRefreshToken && !f.ignoreRevoke {
		f.revoked = true
	}
	return nil
}

func (f *fakeMCP) Probe(_ context.Context, _ string) (int, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probeCalls++
	status := http.StatusOK
	if f.revoked {
		status = http.StatusUnauthorized
	}
	if f.probeStatus != 0 {
		status = f.probeStatus
	}
	return status, f.now.Add(f.probeDateOffset), nil
}

func (f *fakeMCP) Refresh(_ context.Context, _ string) (refreshOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshCalls++
	return refreshOutcome{Dead: f.revoked, ServerDate: f.now.Add(f.probeDateOffset)}, nil
}

func (f *fakeMCP) called(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, call := range f.calls {
		if call == name {
			count++
		}
	}
	return count
}

func (f *fakeMCP) targets() []*fakeResume {
	f.mu.Lock()
	defer f.mu.Unlock()
	var found []*fakeResume
	for _, id := range f.order {
		if id != fakeSourceID && id != fakeOtherID {
			found = append(found, f.resumes[id])
		}
	}
	return found
}

// fakeDocumentBytes and fakePhotoBytes report malformed tool input as a
// closed validation result, as the server does, rather than a Go error.
func fakeDocumentBytes(value any) ([]byte, bool) {
	payload, err := json.Marshal(value)
	return payload, err == nil
}

func fakePhotoBytes(value any) ([]byte, bool) {
	data, err := base64.StdEncoding.DecodeString(fmt.Sprint(value))
	return data, err == nil
}

func fakeResult(value any) *mcp.CallToolResult {
	return &mcp.CallToolResult{StructuredContent: value, Content: []mcp.Content{&mcp.TextContent{Text: "{}"}}}
}

func fakeError(code string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: code}}}
}

func bumpRevision(value string) string {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return value
	}
	return strconv.FormatUint(parsed+1, 10)
}

// fixtureDocument loads the schema's store-valid full fixture.
func fixtureDocument(t *testing.T, withPhoto bool) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packages", "schema", "fixtures", "full.json"))
	if err != nil || len(data) > 64<<10 {
		t.Fatalf("read fixture: %v", err)
	}
	document, _, photo, err := canonicalDocument(data)
	if err != nil || photo == nil {
		t.Fatalf("fixture is not a valid document with a photo: %v", err)
	}
	if withPhoto {
		document.PersonalDetails.Photo = photo
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func withDocumentPhoto(t *testing.T, raw []byte, photo *schema.Photo) []byte {
	t.Helper()
	var document schema.Resume
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	document.PersonalDetails.Photo = photo
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func testImage(width, height int, transparent bool) *image.NRGBA {
	value := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			alpha := uint8(255)
			if transparent && (x+y)%3 == 0 {
				alpha = 96
			}
			value.SetNRGBA(x, y, color.NRGBA{R: uint8(40 + 10*x), G: uint8(60 + 8*y), B: 120, A: alpha})
		}
	}
	return value
}

func testJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, testImage(width, height, false), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func testPNG(t *testing.T, width, height int, transparent bool) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, testImage(width, height, transparent)); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}
