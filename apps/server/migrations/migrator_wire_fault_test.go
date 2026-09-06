package migrations

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type migrationWireFault struct {
	target          string
	listener        *net.TCPListener
	armed           atomic.Bool
	connections     atomic.Int32
	atFault         atomic.Int32
	ready           chan struct{}
	returned        chan struct{}
	closed          chan struct{}
	cancelGate      chan struct{}
	cancelSeen      chan struct{}
	secondSeen      chan struct{}
	secondGate      chan struct{}
	closeOnce       sync.Once
	wg              sync.WaitGroup
	mu              sync.Mutex
	sockets         map[string]*net.TCPConn
	faultSocket     *net.TCPConn
	command         string
	failBeforeWrite bool
	blockSecond     bool
}

func newMigrationWireFault(t *testing.T, target string, commands ...string) *migrationWireFault {
	t.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	fault := &migrationWireFault{
		target: target, listener: listener, ready: make(chan struct{}), returned: make(chan struct{}),
		closed: make(chan struct{}), cancelGate: make(chan struct{}), cancelSeen: make(chan struct{}),
		secondSeen: make(chan struct{}), secondGate: make(chan struct{}),
		sockets: make(map[string]*net.TCPConn),
	}
	fault.command = "COMMIT"
	if len(commands) == 1 {
		fault.command = commands[0]
	}
	fault.wg.Add(1)
	go fault.accept(t)
	t.Cleanup(func() {
		fault.closeOnce.Do(func() { close(fault.cancelGate) })
		select {
		case <-fault.secondGate:
		default:
			close(fault.secondGate)
		}
		if closeErr := listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Errorf("close wire listener: %v", closeErr)
		}
		fault.wg.Wait()
	})
	return fault
}

func (f *migrationWireFault) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", f.listener.Addr().String())
	if err != nil {
		return nil, err
	}
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return nil, errors.New("wire fixture did not create TCP connection")
	}
	f.mu.Lock()
	f.sockets[tcp.LocalAddr().String()] = tcp
	f.mu.Unlock()
	return tcp, nil
}

func (f *migrationWireFault) accept(t *testing.T) {
	defer f.wg.Done()
	for {
		client, err := f.listener.AcceptTCP()
		if err != nil {
			return
		}
		f.wg.Add(1)
		go f.serve(t, client)
	}
}

func (f *migrationWireFault) serve(t *testing.T, client *net.TCPConn) {
	defer f.wg.Done()
	defer closeWireConnection(t, "proxy client", client)
	reader := bufio.NewReader(client)
	packet, err := readStartupPacket(reader)
	if err != nil {
		return
	}
	if len(packet) == 16 && binary.BigEndian.Uint32(packet[4:8]) == 80877102 {
		select {
		case <-f.cancelSeen:
		default:
			close(f.cancelSeen)
		}
		<-f.cancelGate
		server, dialErr := (&net.Dialer{}).DialContext(context.Background(), "tcp", f.target)
		if dialErr == nil {
			defer closeWireConnection(t, "cancel server", server)
			if _, writeErr := server.Write(packet); writeErr != nil {
				return
			}
		}
		return
	}
	connectionNumber := f.connections.Add(1)
	if f.blockSecond && connectionNumber == 2 {
		close(f.secondSeen)
		<-f.secondGate
	}
	server, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", f.target)
	if err != nil {
		return
	}
	defer closeWireConnection(t, "application server", server)
	if _, err := server.Write(packet); err != nil {
		return
	}
	startupReady := make(chan struct{})
	clientEnded := make(chan struct{})
	selected := &atomic.Bool{}
	go f.forwardFrontend(reader, client, server, clientEnded, selected)
	f.forwardBackend(client, server, startupReady, selected)
	<-clientEnded
	if selected.Load() {
		select {
		case <-f.closed:
		default:
			close(f.closed)
		}
	}
}

func closeWireConnection(t *testing.T, name string, conn net.Conn) {
	t.Helper()
	if closeErr := conn.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
		t.Errorf("close %s: %v", name, closeErr)
	}
}

func readStartupPacket(reader *bufio.Reader) ([]byte, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint32(header))
	if size < 8 || size > 10000 {
		return nil, errors.New("invalid startup packet")
	}
	packet := make([]byte, size)
	copy(packet, header)
	_, err := io.ReadFull(reader, packet[4:])
	return packet, err
}

func (f *migrationWireFault) forwardFrontend(reader *bufio.Reader, client, server net.Conn, ended chan struct{}, selected *atomic.Bool) {
	defer close(ended)
	for {
		typeByte, err := reader.ReadByte()
		if err != nil {
			return
		}
		lengthBytes := make([]byte, 4)
		if _, err := io.ReadFull(reader, lengthBytes); err != nil {
			return
		}
		length := int(binary.BigEndian.Uint32(lengthBytes))
		if length < 4 || length > 64<<20 {
			return
		}
		payload := make([]byte, length-4)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return
		}
		frame := append([]byte{typeByte}, lengthBytes...)
		frame = append(frame, payload...)
		if typeByte == 'Q' && isWireQuery(payload, f.command) && f.armed.CompareAndSwap(false, true) {
			selected.Store(true)
			if f.failBeforeWrite {
				f.recordFaultSocket(client)
				f.atFault.Store(f.connections.Load())
				if _, writeErr := client.Write([]byte{'!', 0, 0, 0, 4}); writeErr != nil {
					return
				}
				close(f.returned)
				if deadlineErr := server.SetReadDeadline(time.Now()); deadlineErr != nil {
					return
				}
				for {
					if _, readErr := reader.ReadByte(); readErr != nil {
						return
					}
				}
			}
		}
		if _, err := server.Write(frame); err != nil {
			return
		}
	}
}

func isWireQuery(payload []byte, command string) bool {
	query := strings.TrimSpace(strings.TrimSuffix(string(bytes.TrimSuffix(payload, []byte{0})), ";"))
	normalized := strings.ToUpper(strings.Join(strings.Fields(query), " "))
	if command == "CATALOG_PROBE" {
		return strings.HasPrefix(normalized, "SELECT TO_REGCLASS('PUBLIC.RUNTIME_WRITE_STATE') IS NOT NULL") &&
			strings.Contains(normalized, "TO_REGCLASS('PUBLIC.GOOSE_DB_VERSION') IS NOT NULL") &&
			strings.Contains(normalized, "PG_GET_USERBYID(RELOWNER)")
	}
	return normalized == strings.ToUpper(strings.Join(strings.Fields(command), " "))
}

func (f *migrationWireFault) forwardBackend(client, server net.Conn, startupReady chan struct{}, selected *atomic.Bool) {
	reader := bufio.NewReader(server)
	startup := true
	withholding := false
	for {
		typeByte, err := reader.ReadByte()
		if err != nil {
			return
		}
		lengthBytes := make([]byte, 4)
		if _, err := io.ReadFull(reader, lengthBytes); err != nil {
			return
		}
		length := int(binary.BigEndian.Uint32(lengthBytes))
		if length < 4 || length > 64<<20 {
			return
		}
		payload := make([]byte, length-4)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return
		}
		if selected.Load() {
			withholding = true
		}
		if withholding {
			if typeByte == 'Z' {
				f.recordFaultSocket(client)
				f.atFault.Store(f.connections.Load())
				close(f.ready)
				if _, writeErr := client.Write([]byte{'!', 0, 0, 0, 4}); writeErr != nil {
					return
				}
				close(f.returned)
				return
			}
			continue
		}
		frame := append([]byte{typeByte}, lengthBytes...)
		frame = append(frame, payload...)
		if _, err := client.Write(frame); err != nil {
			return
		}
		if startup && typeByte == 'Z' {
			startup = false
			close(startupReady)
		}
	}
}

func (f *migrationWireFault) recordFaultSocket(client net.Conn) {
	f.mu.Lock()
	f.faultSocket = f.sockets[client.RemoteAddr().String()]
	f.mu.Unlock()
}

func (f *migrationWireFault) assertSocketClosedAtReturn(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	socket := f.faultSocket
	f.mu.Unlock()
	if socket == nil {
		t.Fatal("fault socket was not identified")
	}
	raw, err := socket.SyscallConn()
	if err == nil {
		err = raw.Control(func(uintptr) {})
	}
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("fault socket remained open at return: %v", err)
	}
}

func waitMigrationWire(t *testing.T, event <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for %s", name)
	}
}
