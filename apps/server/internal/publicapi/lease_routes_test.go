package publicapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/publicstate"
)

// TestLeasedPublicRoutes_RevocationEndsABlockedWrite proves a revoking
// transition ends every leased public response blocked on a reader that
// stopped reading, fresh or cached: the lease's cancel hook sets the write
// deadline, the write returns, the handler ends, and the lease drains before
// the transition's deadline (docs/adr/0010-public-artifact-revocation.md).
func TestLeasedPublicRoutes_RevocationEndsABlockedWrite(t *testing.T) {
	for _, test := range leasedRouteCases() {
		t.Run(test.name, func(t *testing.T) {
			route := test.build(t)
			primeLeasedRoute(t, route, test.cached)
			writer := newStatusBlockedWriter()
			t.Cleanup(writer.release)
			done := serveBlocked(t, route, writer)

			transition, err := route.coordinator.Begin(context.Background(), route.plan(publicstate.Revoking))
			if err != nil {
				t.Fatal(err)
			}
			closeErr := transition.Close(context.Background(), time.Now().Add(time.Second))
			writer.release()
			<-done
			if closeErr != nil {
				t.Fatalf("revocation did not end the blocked write and drain the lease: %v", closeErr)
			}
			if !writer.deadlineSet() {
				t.Fatal("revocation did not set the write deadline")
			}
			if err := transition.Rollback(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestLeasedPublicRoutes_OnlyRevocationEndsABlockedWrite proves nothing but
// a revocation cuts a leased write, and that a write blocked with no
// revocation pending holds only its own lease: another viewer of the same
// resume is served in full, and a non-revoking transition closes without
// waiting for it or touching it. The revocation that follows still ends it.
func TestLeasedPublicRoutes_OnlyRevocationEndsABlockedWrite(t *testing.T) {
	for _, test := range leasedRouteCases() {
		t.Run(test.name, func(t *testing.T) {
			route := test.build(t)
			primeLeasedRoute(t, route, test.cached)
			writer := newStatusBlockedWriter()
			t.Cleanup(writer.release)
			done := serveBlocked(t, route, writer)

			other := httptest.NewRecorder()
			route.handler.ServeHTTP(other, newLeaseTestRequest(route.path))
			if other.Code != http.StatusOK || strconv.Itoa(other.Body.Len()) != other.Header().Get("Content-Length") {
				t.Fatalf("another viewer got %d with %d of %s bytes, want the whole 200 body", other.Code, other.Body.Len(), other.Header().Get("Content-Length"))
			}
			if route.resumeID != uuid.Nil {
				edit, err := route.coordinator.Begin(context.Background(), route.plan(publicstate.NonDraining))
				if err != nil {
					t.Fatal(err)
				}
				if err := edit.Close(context.Background(), time.Now().Add(time.Second)); err != nil {
					t.Fatalf("a non-revoking transition waited for the blocked write: %v", err)
				}
				if err := edit.Rollback(); err != nil {
					t.Fatal(err)
				}
			}
			if writer.deadlineSet() {
				t.Fatal("a write deadline was set with no revocation")
			}

			revocation, err := route.coordinator.Begin(context.Background(), route.plan(publicstate.Revoking))
			if err != nil {
				t.Fatal(err)
			}
			if err := revocation.Close(context.Background(), time.Now().Add(time.Second)); err != nil {
				t.Fatalf("revocation did not end the blocked write and drain the lease: %v", err)
			}
			<-done
			if err := revocation.Rollback(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestLeasedPublicRoutes_HealthyResponseSetsNoDeadline proves the leased
// write arms nothing on a response whose lease is never revoked: the whole
// body is written and no write deadline is ever set.
func TestLeasedPublicRoutes_HealthyResponseSetsNoDeadline(t *testing.T) {
	for _, test := range leasedRouteCases() {
		t.Run(test.name, func(t *testing.T) {
			route := test.build(t)
			primeLeasedRoute(t, route, test.cached)
			writer := &deadlineCountingRecorder{ResponseRecorder: httptest.NewRecorder()}
			route.handler.ServeHTTP(writer, newLeaseTestRequest(route.path))
			if writer.Code != http.StatusOK || writer.Body.Len() == 0 || strconv.Itoa(writer.Body.Len()) != writer.Header().Get("Content-Length") {
				t.Fatalf("response = %d with %d of %s bytes, want the whole 200 body", writer.Code, writer.Body.Len(), writer.Header().Get("Content-Length"))
			}
			if count := writer.deadlines.Load(); count != 0 {
				t.Fatalf("a healthy response set %d write deadline(s), want none", count)
			}
		})
	}
}

// deadlineCountingRecorder records a response and counts the write
// deadlines http.ResponseController sets on it.
type deadlineCountingRecorder struct {
	*httptest.ResponseRecorder
	deadlines atomic.Int32
}

func (w *deadlineCountingRecorder) SetWriteDeadline(time.Time) error {
	w.deadlines.Add(1)
	return nil
}
