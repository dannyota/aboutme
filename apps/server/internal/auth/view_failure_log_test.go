package auth

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/internal/store"
)

type failingViewDB struct{}

const failingViewDBMessage = "SENSITIVE-message-text"

func (failingViewDB) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, &pgconn.PgError{Code: "57014", Message: failingViewDBMessage}
}

func (failingViewDB) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	return nil, &pgconn.PgError{Code: "57014", Message: failingViewDBMessage}
}

func (failingViewDB) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	return failingViewRow{}
}

type failingViewRow struct{}

func (failingViewRow) Scan(...interface{}) error {
	return &pgconn.PgError{Code: "57014", Message: failingViewDBMessage}
}

func TestRedirectViewFailure_ReadFailureLogsLikeSuccessPath(t *testing.T) {
	t.Parallel()

	var logBuf bytes.Buffer
	svc := &Service{
		q:            store.New(failingViewDB{}),
		logger:       slog.New(slog.NewJSONHandler(&logBuf, nil)),
		publicOrigin: "https://aboutme.example",
	}
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/auth/google/callback", nil)
	recorder := httptest.NewRecorder()

	svc.redirectViewFailure(recorder, request, Transaction{Provider: ProviderGoogle, Purpose: PurposeView, ResumeID: uuid.New()}, "auth_failed")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	logged := logBuf.String()
	for _, want := range []string{`"request_id"`, `"provider":"google"`, `"op":"view_failure_read_resume"`, `"sqlstate":"57014"`} {
		if !strings.Contains(logged, want) {
			t.Errorf("log record = %q, want it to contain %s", logged, want)
		}
	}
	if strings.Contains(logged, failingViewDBMessage) {
		t.Errorf("log record = %q, leaked the database error text", logged)
	}
}
