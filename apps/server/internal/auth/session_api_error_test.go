package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/internal/api"
)

func TestWriteSessionAPIInternalError(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	deadline, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()

	cases := []struct {
		name     string
		ctx      context.Context
		err      error
		status   int
		level    string
		message  string
		sqlstate string
	}{
		{"client abort", canceled, fmt.Errorf("wrapped: %w", context.Canceled), 499, "INFO", "auth: session api request canceled", ""},
		{"deadline", deadline, context.DeadlineExceeded, http.StatusInternalServerError, "ERROR", "auth: session api internal error", ""},
		{"canceled error with live request", context.Background(), context.Canceled, http.StatusInternalServerError, "ERROR", "auth: session api internal error", ""},
		{"plain error", canceled, errors.New("private error text"), http.StatusInternalServerError, "ERROR", "auth: session api internal error", ""},
		{"database error", context.Background(), &pgconn.PgError{Code: "23505", Message: "private error text"}, http.StatusInternalServerError, "ERROR", "auth: session api internal error", "23505"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			svc := &Service{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(tc.ctx, http.MethodGet, "/api/v1/me", nil)
			handler := api.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				svc.writeSessionAPIInternalError(w, r, "get_user", tc.err)
			}))
			handler.ServeHTTP(recorder, request)

			if recorder.Code != tc.status {
				t.Errorf("status = %d, want %d", recorder.Code, tc.status)
			}
			if tc.status == 499 && recorder.Body.Len() != 0 {
				t.Errorf("body = %q, want empty", recorder.Body.String())
			}
			if tc.status == http.StatusInternalServerError && !strings.Contains(recorder.Body.String(), `"code":"internal_error"`) {
				t.Errorf("body = %q, want opaque internal error", recorder.Body.String())
			}
			var record map[string]any
			if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
				t.Fatalf("decode log: %v; log = %q", err, logs.String())
			}
			for key, want := range map[string]string{
				"level": tc.level, "msg": tc.message, "request_id": recorder.Header().Get(api.RequestIDHeader), "op": "get_user",
			} {
				if record[key] != want {
					t.Errorf("log %s = %v, want %q", key, record[key], want)
				}
			}
			if recorder.Header().Get(api.RequestIDHeader) == "" {
				t.Error("request ID is empty")
			}
			if tc.sqlstate != "" && record["sqlstate"] != tc.sqlstate {
				t.Errorf("sqlstate = %v, want %q", record["sqlstate"], tc.sqlstate)
			}
			wantFields := 5
			if tc.sqlstate != "" {
				wantFields++
			}
			if len(record) != wantFields || strings.Contains(logs.String(), "private error text") {
				t.Errorf("unexpected log fields or error text: %s", logs.String())
			}
		})
	}
}
