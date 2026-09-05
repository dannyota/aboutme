package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const notificationChannel = "aboutme_resume_revision"

type listenerConn interface {
	listen(context.Context) error
	wait(context.Context) (*pgconn.Notification, error)
	close(context.Context)
}

type listenerDeps struct {
	acquire func(context.Context, *store.Pool) (listenerConn, error)
	backoff func(context.Context, time.Duration) bool
}

type poolListenerConn struct{ c *pgxpool.Conn }

func (c *poolListenerConn) listen(ctx context.Context) error {
	_, err := c.c.Exec(ctx, "LISTEN "+notificationChannel)
	return err
}
func (c *poolListenerConn) wait(ctx context.Context) (*pgconn.Notification, error) {
	return c.c.Conn().WaitForNotification(ctx)
}
func (c *poolListenerConn) close(ctx context.Context) {
	defer c.c.Release()
	if err := c.c.Conn().Close(ctx); err != nil {
		return
	}
}

func closeListenerConn(ctx context.Context, c listenerConn) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	c.close(cleanupCtx)
}

func acquireListenerConn(ctx context.Context, p *store.Pool) (listenerConn, error) {
	c, err := p.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	return &poolListenerConn{c: c}, nil
}

func waitBackoff(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func stopListenerIfCanceled(ctx context.Context, hub *Hub) bool {
	if ctx.Err() == nil {
		return false
	}
	hub.SetAvailable(false)
	return true
}

// RunListener keeps one pool connection in LISTEN mode and forwards validated
// revision metadata to hub. Cancellation returns nil after releasing it.
func RunListener(ctx context.Context, pool *store.Pool, hub *Hub) error {
	return runListener(ctx, pool, hub, listenerDeps{acquire: acquireListenerConn, backoff: waitBackoff})
}

func runListener(ctx context.Context, pool *store.Pool, hub *Hub, deps listenerDeps) error {
	if pool == nil || hub == nil {
		return errors.New("realtime: nil listener dependency")
	}
	if deps.acquire == nil || deps.backoff == nil {
		return errors.New("realtime: nil listener function")
	}
	backoff := 100 * time.Millisecond
	for {
		if stopListenerIfCanceled(ctx, hub) {
			return nil
		}
		conn, err := deps.acquire(ctx, pool)
		if err == nil && conn == nil {
			err = errors.New("realtime: listener acquire returned nil connection")
		}
		if err == nil {
			err = conn.listen(ctx)
		}
		if err != nil {
			hub.SetAvailable(false)
		}
		if err == nil {
			hub.SetAvailable(true)
			for {
				n, waitErr := conn.wait(ctx)
				if waitErr != nil {
					hub.SetAvailable(false)
					closeListenerConn(ctx, conn)
					if stopListenerIfCanceled(ctx, hub) {
						return nil
					}
					err = waitErr
					break
				}
				change, parseErr := parseNotification(n)
				if parseErr != nil {
					hub.SetAvailable(false)
					closeListenerConn(ctx, conn)
					err = parseErr
					break
				}
				hub.Publish(change)
				backoff = 100 * time.Millisecond
			}
		} else if conn != nil {
			closeListenerConn(ctx, conn)
			hub.SetAvailable(false)
		}
		if stopListenerIfCanceled(ctx, hub) {
			return nil
		}
		_ = err
		if !deps.backoff(ctx, backoff) {
			hub.SetAvailable(false)
			return nil
		}
		if backoff < 5*time.Second {
			backoff *= 2
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		}
	}
}

func parseNotification(n *pgconn.Notification) (Change, error) {
	if n == nil || n.Channel != notificationChannel || len(n.Payload) > 512 {
		return Change{}, errors.New("realtime: malformed notification")
	}
	fields, err := notificationFields([]byte(n.Payload))
	if err != nil || len(fields) != 4 {
		return Change{}, errors.New("realtime: malformed notification")
	}
	for _, k := range []string{"account_id", "resume_id", "revision", "deleted"} {
		value, ok := fields[k]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return Change{}, errors.New("realtime: malformed notification")
		}
	}
	var v struct {
		AccountID uuid.UUID `json:"account_id"`
		ResumeID  uuid.UUID `json:"resume_id"`
		Revision  int64     `json:"revision"`
		Deleted   bool      `json:"deleted"`
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(n.Payload)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return Change{}, errors.New("realtime: malformed notification")
	}
	var extra interface{}
	if err := dec.Decode(&extra); err != io.EOF {
		return Change{}, errors.New("realtime: malformed notification")
	}
	if v.AccountID == uuid.Nil || v.ResumeID == uuid.Nil || v.Revision <= 0 {
		return Change{}, errors.New("realtime: malformed notification")
	}
	return Change{AccountID: v.AccountID, ResumeID: v.ResumeID, Revision: v.Revision, Deleted: v.Deleted}, nil
}

func notificationFields(payload []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(payload))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, errors.New("malformed object")
	}
	fields := make(map[string]json.RawMessage, 4)
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name, ok := key.(string)
		if !ok {
			return nil, errors.New("malformed key")
		}
		if _, exists := fields[name]; exists {
			return nil, errors.New("duplicate key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		fields[name] = raw
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	var extra interface{}
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("trailing data")
	}
	return fields, nil
}
