// Package pgtransport captures and consumes close-only PostgreSQL TCP capabilities.
package pgtransport

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// Capability is an opaque, one-shot close capability for one pgx TCP transport.
type Capability struct {
	mu          sync.Mutex
	tcp         *net.TCPConn
	cleanupDone <-chan struct{}
	consumed    bool
}

// Result reports whether retirement proved local transport closure.
type Result struct {
	PhysicalClosed bool
	CleanupDone    bool
	Err            error
}

// ReleaseClean validates the binding, performs the sole logical close, and disarms c.
func (c *Capability) ReleaseClean(conn *sql.Conn) error {
	if c == nil || conn == nil {
		return errors.New("nil clean release")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.consumed || c.tcp == nil {
		return errors.New("retirement capability already consumed")
	}
	err := conn.Raw(func(raw any) error {
		stdlibConn, ok := raw.(*stdlib.Conn)
		if !ok || stdlibConn == nil || stdlibConn.Conn() == nil || stdlibConn.Conn().PgConn() == nil {
			return fmt.Errorf("clean release driver binding mismatch %T", raw)
		}
		pgConn := stdlibConn.Conn().PgConn()
		tcp, captureErr := exactTCP(pgConn.Conn())
		if captureErr != nil || tcp != c.tcp || pgConn.CleanupDone() != c.cleanupDone {
			return errors.Join(errors.New("clean release transport binding mismatch"), captureErr)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := conn.Close(); err != nil {
		return err
	}
	c.consumed = true
	c.tcp = nil
	return nil
}

// ReleaseCleanPGX validates a clean pgx binding and disarms c without closing conn.
// The caller may return the exclusively held connection to its pool afterward.
func (c *Capability) ReleaseCleanPGX(conn *pgx.Conn) error {
	if c == nil || conn == nil || conn.PgConn() == nil {
		return errors.New("nil clean pgx release")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.consumed || c.tcp == nil {
		return errors.New("retirement capability already consumed")
	}
	tcp, err := exactTCP(conn.PgConn().Conn())
	if err != nil || tcp != c.tcp || conn.PgConn().CleanupDone() != c.cleanupDone {
		return errors.Join(errors.New("clean pgx release transport binding mismatch"), err)
	}
	c.consumed = true
	c.tcp = nil
	return nil
}

// Capture records the exact close-only transport before any SQL is executed.
func Capture(conn *sql.Conn) (*Capability, error) {
	if conn == nil {
		return nil, errors.New("nil SQL connection")
	}
	var capability *Capability
	err := conn.Raw(func(raw any) error {
		stdlibConn, ok := raw.(*stdlib.Conn)
		if !ok || stdlibConn == nil || stdlibConn.Conn() == nil || stdlibConn.Conn().PgConn() == nil {
			return fmt.Errorf("unsupported SQL driver connection %T", raw)
		}
		var err error
		capability, err = CapturePGX(stdlibConn.Conn())
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return capability, nil
}

// CapturePGX records the close-only transport from an exclusively owned pgx connection.
func CapturePGX(conn *pgx.Conn) (*Capability, error) {
	if conn == nil || conn.PgConn() == nil {
		return nil, errors.New("nil pgx connection")
	}
	pgConn := conn.PgConn()
	tcp, err := exactTCP(pgConn.Conn())
	if err != nil {
		return nil, err
	}
	return &Capability{tcp: tcp, cleanupDone: pgConn.CleanupDone()}, nil
}

// RetirePGX consumes c for an exclusively owned pgx connection.
func (c *Capability) RetirePGX(ctx context.Context, conn *pgx.Conn) Result {
	return c.retire(ctx, nil, conn)
}

// Retire consumes c and proves closure through CleanupDone or its exact TCP socket.
func (c *Capability) Retire(ctx context.Context, conn *sql.Conn) Result {
	return c.retire(ctx, conn, nil)
}

func (c *Capability) retire(ctx context.Context, sqlConn *sql.Conn, direct *pgx.Conn) Result {
	if c == nil {
		return Result{Err: errors.New("nil retirement capability")}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.consumed || c.tcp == nil {
		return Result{Err: errors.New("retirement capability already consumed")}
	}
	c.consumed = true
	tcp := c.tcp
	c.tcp = nil
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var closeErr, bindingErr error
	if direct != nil {
		if direct.PgConn() == nil {
			bindingErr = errors.New("retirement pgx binding mismatch")
		} else {
			boundTCP, err := exactTCP(direct.PgConn().Conn())
			if err != nil || boundTCP != tcp || direct.PgConn().CleanupDone() != c.cleanupDone {
				bindingErr = errors.Join(errors.New("retirement transport binding mismatch"), err)
			} else {
				closeErr = direct.Close(cleanupCtx)
			}
		}
	} else if sqlConn != nil {
		rawErr := sqlConn.Raw(func(raw any) error {
			stdlibConn, ok := raw.(*stdlib.Conn)
			if !ok || stdlibConn == nil || stdlibConn.Conn() == nil || stdlibConn.Conn().PgConn() == nil {
				bindingErr = fmt.Errorf("retirement driver binding mismatch %T", raw)
				return nil
			}
			pgConn := stdlibConn.Conn().PgConn()
			boundTCP, err := exactTCP(pgConn.Conn())
			if err != nil || boundTCP != tcp || pgConn.CleanupDone() != c.cleanupDone {
				bindingErr = errors.Join(errors.New("retirement transport binding mismatch"), err)
				return nil
			}
			closeErr = stdlibConn.Conn().Close(cleanupCtx)
			return nil
		})
		if rawErr != nil && !errors.Is(rawErr, sql.ErrConnDone) {
			bindingErr = errors.Join(bindingErr, rawErr)
		}
	}
	if cleanupClosed(c.cleanupDone) {
		return Result{PhysicalClosed: true, CleanupDone: true, Err: errors.Join(bindingErr, closeErr)}
	}
	fenceErr := errors.Join(tcp.SetLinger(0), tcp.SetDeadline(time.Now()))
	tcpErr := tcp.Close()
	closed := tcpErr == nil || errors.Is(tcpErr, net.ErrClosed)
	done := cleanupClosed(c.cleanupDone)
	if !closed && !done {
		return Result{Err: errors.Join(bindingErr, closeErr, tcpErr, errors.New("physical transport closure was not confirmed"))}
	}
	return Result{PhysicalClosed: true, CleanupDone: done, Err: errors.Join(bindingErr, closeErr, fenceErr, nonClosedError(tcpErr))}
}

func exactTCP(conn net.Conn) (*net.TCPConn, error) {
	switch conn := conn.(type) {
	case *net.TCPConn:
		if conn == nil {
			return nil, errors.New("nil TCP transport")
		}
		return conn, nil
	case *tls.Conn:
		if conn == nil {
			return nil, errors.New("nil TLS transport")
		}
		tcp, ok := conn.NetConn().(*net.TCPConn)
		if !ok || tcp == nil {
			return nil, fmt.Errorf("unsupported TLS transport %T", conn.NetConn())
		}
		return tcp, nil
	default:
		return nil, fmt.Errorf("unsupported PostgreSQL transport %T", conn)
	}
}

func cleanupClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func nonClosedError(err error) error {
	if err == nil || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
