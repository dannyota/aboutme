package store

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const postgresCancelRequestCode = 80877102

type writeRetirementProxy struct {
	listener        *net.TCPListener
	upstream        string
	cancelStarted   chan struct{}
	releaseCancel   chan struct{}
	dataClosed      chan struct{}
	closeOnce       sync.Once
	cancelOnce      sync.Once
	dataCloseOnce   sync.Once
	dataConnections atomic.Int32
}

func newWriteRetirementProxy(t *testing.T, upstream string) *writeRetirementProxy {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	p := &writeRetirementProxy{
		listener:      listener,
		upstream:      upstream,
		cancelStarted: make(chan struct{}),
		releaseCancel: make(chan struct{}),
		dataClosed:    make(chan struct{}),
	}
	go p.serve()
	t.Cleanup(func() {
		p.closeOnce.Do(func() { close(p.releaseCancel) })
		ignoreWriteCleanupError(p.listener.Close())
	})
	return p
}

func (p *writeRetirementProxy) address() string { return p.listener.Addr().String() }

func (p *writeRetirementProxy) serve() {
	for {
		client, err := p.listener.AcceptTCP()
		if err != nil {
			return
		}
		go p.serveConnection(client)
	}
}

func (p *writeRetirementProxy) serveConnection(client *net.TCPConn) {
	defer client.Close() //nolint:errcheck
	header := make([]byte, 8)
	if _, err := io.ReadFull(client, header); err != nil {
		return
	}
	if binary.BigEndian.Uint32(header[4:]) == postgresCancelRequestCode {
		p.cancelOnce.Do(func() { close(p.cancelStarted) })
		<-p.releaseCancel
		return
	}
	p.dataConnections.Add(1)
	var dialer net.Dialer
	upstream, err := dialer.DialContext(context.Background(), "tcp4", p.upstream)
	if err != nil {
		return
	}
	defer upstream.Close() //nolint:errcheck
	if _, err := upstream.Write(header); err != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_, copyErr := io.Copy(client, upstream)
		ignoreWriteCleanupError(copyErr)
		close(done)
	}()
	_, copyErr := io.Copy(upstream, client)
	ignoreWriteCleanupError(copyErr)
	p.dataCloseOnce.Do(func() { close(p.dataClosed) })
	ignoreWriteCleanupError(upstream.Close())
	<-done
}

func assertTCPClosed(t *testing.T, conn *net.TCPConn) {
	t.Helper()
	raw, err := conn.SyscallConn()
	if errors.Is(err, net.ErrClosed) {
		return
	}
	if err != nil {
		t.Fatalf("inspect original TCP socket: %v", err)
	}
	err = raw.Control(func(uintptr) {})
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("original TCP socket remains open at operation return: %v", err)
	}
}

func TestWriteTxRunnerRetiresOriginalSocketBeforeAmbiguousReturn(t *testing.T) {
	dsn, admin := newWriteRunnerDatabase(t)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	upstream := net.JoinHostPort(cfg.ConnConfig.Host, strconv.Itoa(int(cfg.ConnConfig.Port)))
	proxy := newWriteRetirementProxy(t, upstream)
	cfg.MaxConns = 1
	cfg.MinConns = 0
	operationCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	noticeSeen := make(chan struct{})
	var noticeOnce sync.Once
	cfg.ConnConfig.OnNotice = func(_ *pgconn.PgConn, notice *pgconn.Notice) {
		if notice.Message == "aboutme-write-retirement-ready" {
			noticeOnce.Do(func() {
				close(noticeSeen)
				cancel()
			})
		}
	}
	dataClient := make(chan *net.TCPConn, 1)
	var captureDataClient sync.Once
	cfg.ConnConfig.DialFunc = func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		conn, dialErr := dialer.DialContext(ctx, "tcp4", proxy.address())
		if dialErr != nil {
			return nil, dialErr
		}
		tcp, ok := conn.(*net.TCPConn)
		if !ok {
			ignoreWriteCleanupError(conn.Close())
			return nil, errors.New("loopback dial did not return TCP connection")
		}
		captureDataClient.Do(func() { dataClient <- tcp })
		return tcp, nil
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, setupErr := conn.Exec(ctx, `SET SESSION AUTHORIZATION aboutme_app`)
		return setupErr
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	runner := NewWriteTxRunner(pool)
	callbacks := 0
	var oldPID int32
	err = runner.ExecWrite(operationCtx, func(q *Queries) error {
		callbacks++
		if queryErr := q.db.QueryRow(operationCtx, `SELECT pg_backend_pid()`).Scan(&oldPID); queryErr != nil {
			return queryErr
		}
		if _, insertErr := q.db.Exec(operationCtx, `INSERT INTO public.runtime_write_probe VALUES (40)`); insertErr != nil {
			return insertErr
		}
		_, sleepErr := q.db.Exec(operationCtx, `DO $$ BEGIN RAISE NOTICE 'aboutme-write-retirement-ready'; PERFORM pg_sleep(30); END $$`)
		return sleepErr
	})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want context canceled", err)
	}
	if callbacks != 1 {
		t.Fatalf("callbacks=%d, want 1", callbacks)
	}
	select {
	case <-noticeSeen:
	default:
		t.Fatal("operation canceled before PostgreSQL reported query progress")
	}
	var originalTCP *net.TCPConn
	select {
	case originalTCP = <-dataClient:
	default:
		t.Fatal("original TCP socket was not captured")
	}
	assertTCPClosed(t, originalTCP)
	select {
	case <-proxy.cancelStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("pgx asynchronous CancelRequest did not start")
	}
	select {
	case <-proxy.dataClosed:
	case <-time.After(2 * time.Second):
		t.Fatal("proxy did not observe original data socket closure")
	}
	if got := proxy.dataConnections.Load(); got != 1 {
		t.Fatalf("data connections=%d before explicit retry, want 1", got)
	}
	var rows int
	if err := admin.QueryRowContext(context.Background(), `SELECT count(*) FROM public.runtime_write_probe WHERE id=40`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("ambiguous transaction rows=%d err=%v, want zero", rows, err)
	}
	proxy.closeOnce.Do(func() { close(proxy.releaseCancel) })
	var replacementPID int32
	if err := runner.ExecWrite(context.Background(), func(q *Queries) error {
		return q.db.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&replacementPID)
	}); err != nil {
		t.Fatal(err)
	}
	if replacementPID == oldPID {
		t.Fatalf("retired backend PID %d was reused", oldPID)
	}
	if got := proxy.dataConnections.Load(); got != 2 {
		t.Fatalf("data connections=%d after explicit operation, want 2", got)
	}
}
