package routetable

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

type h2EventKind string

const (
	h2Headers h2EventKind = "HEADERS"
	h2Data    h2EventKind = "DATA"
	h2Reset   h2EventKind = "RST_STREAM"
	h2GoAway  h2EventKind = "GOAWAY"
	h2Closed  h2EventKind = "connection closed"
)

type h2Event struct {
	kind      h2EventKind
	streamID  uint32
	status    string
	length    int
	endStream bool
	code      http2.ErrCode
	err       error
}

func (e h2Event) String() string {
	return fmt.Sprintf("%s stream=%d status=%q length=%d end_stream=%t code=%v err=%v", e.kind, e.streamID, e.status, e.length, e.endStream, e.code, e.err)
}

// HTTP/2 flow control covers only DATA frames, so a zero stream window still lets the server send headers (RFC 9113 section 5.2).
type h2Client struct {
	conn   net.Conn
	framer *http2.Framer
	mu     sync.Mutex
	events chan h2Event
	stop   chan struct{}
}

func dialH2C(t *testing.T, address string) *h2Client {
	t.Helper()
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", address)
	if err != nil {
		t.Fatalf("dialing %s: %v", address, err)
	}
	client := &h2Client{conn: conn, framer: http2.NewFramer(conn, conn), events: make(chan h2Event, 4096), stop: make(chan struct{})}
	client.framer.ReadMetaHeaders = hpack.NewDecoder(4096, nil)
	t.Cleanup(func() {
		close(client.stop)
		if closeErr := conn.Close(); closeErr != nil {
			t.Logf("closing the HTTP/2 client connection: %v", closeErr)
		}
	})
	if _, err := io.WriteString(conn, http2.ClientPreface); err != nil {
		t.Fatalf("writing the client preface: %v", err)
	}
	client.write(t, func(framer *http2.Framer) error {
		return framer.WriteSettings(http2.Setting{ID: http2.SettingInitialWindowSize, Val: 0})
	})
	go client.readLoop()
	return client
}

func (c *h2Client) write(t *testing.T, write func(*http2.Framer) error) {
	t.Helper()
	if err := c.writeFrame(write); err != nil {
		t.Fatalf("writing an HTTP/2 frame: %v", err)
	}
}

func (c *h2Client) writeFrame(write func(*http2.Framer) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return write(c.framer)
}

func (c *h2Client) readLoop() {
	for {
		frame, err := c.framer.ReadFrame()
		if err != nil {
			c.report(h2Event{kind: h2Closed, err: err})
			return
		}
		switch f := frame.(type) {
		case *http2.SettingsFrame:
			if !f.IsAck() {
				err = c.writeFrame(func(framer *http2.Framer) error { return framer.WriteSettingsAck() })
			}
		case *http2.PingFrame:
			if !f.IsAck() {
				err = c.writeFrame(func(framer *http2.Framer) error { return framer.WritePing(true, f.Data) })
			}
		case *http2.MetaHeadersFrame:
			c.report(h2Event{kind: h2Headers, streamID: f.StreamID, status: f.PseudoValue("status"), endStream: f.StreamEnded()})
		case *http2.DataFrame:
			c.report(h2Event{kind: h2Data, streamID: f.StreamID, length: len(f.Data()), endStream: f.StreamEnded()})
		case *http2.RSTStreamFrame:
			c.report(h2Event{kind: h2Reset, streamID: f.StreamID, code: f.ErrCode})
		case *http2.GoAwayFrame:
			c.report(h2Event{kind: h2GoAway, streamID: f.LastStreamID, code: f.ErrCode})
		}
		if err != nil {
			c.report(h2Event{kind: h2Closed, err: err})
			return
		}
	}
}

func (c *h2Client) report(event h2Event) {
	select {
	case c.events <- event:
	case <-c.stop:
	}
}

func (c *h2Client) get(t *testing.T, authority, path string) {
	t.Helper()
	var block bytes.Buffer
	encoder := hpack.NewEncoder(&block)
	for _, field := range []hpack.HeaderField{
		{Name: ":method", Value: "GET"}, {Name: ":scheme", Value: "http"},
		{Name: ":authority", Value: authority}, {Name: ":path", Value: path},
	} {
		if err := encoder.WriteField(field); err != nil {
			t.Fatal(err)
		}
	}
	c.write(t, func(framer *http2.Framer) error {
		return framer.WriteHeaders(http2.HeadersFrameParam{StreamID: 1, BlockFragment: block.Bytes(), EndStream: true, EndHeaders: true})
	})
}

func (c *h2Client) next(t *testing.T, timeout time.Duration) h2Event {
	t.Helper()
	select {
	case event := <-c.events:
		return event
	case <-time.After(timeout):
		t.Fatalf("no HTTP/2 frame within %s", timeout)
		return h2Event{}
	}
}

func (c *h2Client) awaitHeaders(t *testing.T) h2Event {
	t.Helper()
	for {
		event := c.next(t, 10*time.Second)
		if event.kind == h2Headers && event.streamID == 1 {
			return event
		}
		if event.kind != h2Headers {
			t.Fatalf("got %s before stream 1's response headers", event)
		}
	}
}

func (c *h2Client) endWhileWindowZero(t *testing.T, wait time.Duration) (h2Event, bool) {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case event := <-c.events:
			switch {
			case event.kind == h2Data && event.streamID == 1:
				t.Fatalf("got %s while the stream window was 0", event)
			case event.kind == h2Reset && event.streamID == 1, event.kind == h2GoAway, event.kind == h2Closed:
				return event, true
			}
		case <-deadline:
			return h2Event{}, false
		}
	}
}

func (c *h2Client) openWindow(t *testing.T) {
	t.Helper()
	c.write(t, func(framer *http2.Framer) error {
		if err := framer.WriteWindowUpdate(0, 1<<30); err != nil {
			return err
		}
		return framer.WriteWindowUpdate(1, 1<<30)
	})
}

func (c *h2Client) readStream(t *testing.T) (int, h2Event) {
	t.Helper()
	received := 0
	for {
		event := c.next(t, 10*time.Second)
		switch {
		case event.kind == h2Data && event.streamID == 1:
			received += event.length
			if event.endStream {
				return received, event
			}
		case event.kind == h2Reset && event.streamID == 1, event.kind == h2GoAway, event.kind == h2Closed:
			return received, event
		}
	}
}
