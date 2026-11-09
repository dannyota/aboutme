package resumeapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/media"
)

// photoHeldBody blocks its first Read until release closes, so an upload
// holds the photo permit while it waits for the body.
type photoHeldBody struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

// Read signals entered on its first call, then blocks until release closes.
func (b *photoHeldBody) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return 0, errors.New("held photo body released")
}

// Close implements io.ReadCloser.
func (*photoHeldBody) Close() error { return nil }

// TestPhotoAdmissionSharedByUploadPaths builds one Service and shows that its
// REST upload handler and its agent upload path take the same photo permit:
// while a REST upload holds it, an agent upload waits and reports media_busy.
func TestPhotoAdmissionSharedByUploadPaths(t *testing.T) {
	service := New(nil, nil, nil, nil, Options{PhotoAdmission: media.NewPhotoAdmission()})
	resumeID := uuid.New()

	body := &photoHeldBody{entered: make(chan struct{}), release: make(chan struct{})}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/resumes/"+resumeID.String()+"/photo", nil)
	req.Body = body
	req.ContentLength = -1
	req.Header.Set("Content-Type", "multipart/form-data; boundary=held")
	req.Header.Set("Idempotency-Key", uuid.NewString())
	req.Header.Set("If-Match", `"r1"`)
	req.SetPathValue("id", resumeID.String())
	recorder := &photoDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	restDone := make(chan struct{})
	go func() {
		defer close(restDone)
		service.handleUploadResumePhoto(recorder, req)
	}()
	var releaseBody sync.Once
	finishREST := func() {
		releaseBody.Do(func() { close(body.release) })
		<-restDone
	}
	defer finishREST()
	select {
	case <-body.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("REST upload never reached its body read")
	}

	principal, err := NewAgentPrincipal(uuid.New(), uuid.New(), uuid.New(), [32]byte{1})
	if err != nil {
		t.Fatalf("NewAgentPrincipal: %v", err)
	}
	agent := service.ExecuteAgent(context.Background(), principal, AgentCall{
		Operation: AgentUploadPhoto, IdempotencyKey: uuid.NewString(),
		ResumeID: resumeID.String(), Revision: "1", File: []byte("photo"),
	})
	if agent.Status != http.StatusServiceUnavailable || !strings.Contains(string(agent.Body), `"media_busy"`) {
		t.Fatalf("agent upload while REST holds the permit = %d %s, want 503 media_busy", agent.Status, agent.Body)
	}

	finishREST()
	if recorder.Code == http.StatusServiceUnavailable {
		t.Fatalf("REST upload = %d %s, want it to have held the permit", recorder.Code, recorder.Body.Bytes())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	release, err := service.photoAdmission.Acquire(ctx)
	if err != nil {
		t.Fatalf("photo permit still held after the REST upload returned: %v", err)
	}
	release()
}
