package resumeapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/printrender"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

func TestOwnerPDFWithDisabledPrintQueue(t *testing.T) {
	queue, err := renderjob.New(renderjob.Config{Renderer: &printrender.Renderer{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	row := resume.Resume{ID: uuid.MustParse("20000000-0000-4000-8000-000000000001")}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		response := httptest.NewRecorder()
		pdfService(row, queue, nil).handleDownloadResumePDF(response, pdfRequest(t, method, row.ID, nil))
		if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" ||
			!strings.Contains(response.Body.String(), `"code":"internal_error"`) {
			t.Fatalf("%s = %d %s", method, response.Code, response.Body.String())
		}
	}
}
