package authmail

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	stubBanner   = "banner-secret-host ESMTP"
	stubUser     = "smtp-user-secret"
	stubPassword = "smtp-password-secret"
	stubFrom     = "danny@aboutme.vn"
	stubTo       = "recipient-secret@example.com"
)

// testPKI issues a throwaway CA and a 127.0.0.1 leaf at test time, so no key
// material is ever committed.
func testPKI(t *testing.T) (*x509.CertPool, tls.Certificate) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test ca"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature,
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return pool, tls.Certificate{Certificate: [][]byte{leafDER}, PrivateKey: leafKey}
}

// stubCommand is one command the stub received and whether TLS was up.
type stubCommand struct {
	line string
	tls  bool
}

// smtpStub is a scripted SMTP server on 127.0.0.1:0. replies overrides the
// reply to a verb ("AUTH", "MAIL", "RCPT", "DATA", or "END" for the reply after
// the message body).
type smtpStub struct {
	implicit, offerSTARTTLS, silent bool
	replies                         map[string]string
	cert                            tls.Certificate
	ln                              net.Listener

	mu       sync.Mutex
	conns    int
	commands []stubCommand
	data     [][]byte
}

func startStub(t *testing.T, cert tls.Certificate, configure func(*smtpStub)) *smtpStub {
	t.Helper()
	s := &smtpStub{offerSTARTTLS: true, replies: map[string]string{}, cert: cert}
	configure(s)
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.ln = ln
	t.Cleanup(func() { closeQuietly(ln) })
	go s.serve()
	return s
}

func (s *smtpStub) port() int {
	addr, ok := s.ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0
	}
	return addr.Port
}

func (s *smtpStub) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns++
		s.mu.Unlock()
		go s.handle(conn)
	}
}

func (s *smtpStub) handle(raw net.Conn) {
	defer closeQuietly(raw)
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{s.cert}, MinVersion: tls.VersionTLS12}
	conn, isTLS := raw, false
	if s.implicit {
		tc := tls.Server(raw, tlsCfg)
		if tc.HandshakeContext(context.Background()) != nil {
			return
		}
		conn, isTLS = tc, true
	}
	if s.silent {
		if _, err := io.Copy(io.Discard, conn); err != nil {
			return
		}
		return
	}
	r := bufio.NewReader(conn)
	reply := func(line string) {
		if _, err := io.WriteString(conn, line+"\r\n"); err != nil {
			return
		}
	}
	pick := func(verb, def string) string {
		if v, ok := s.replies[verb]; ok {
			return v
		}
		return def
	}
	reply("220 " + stubBanner)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		s.mu.Lock()
		s.commands = append(s.commands, stubCommand{line: line, tls: isTLS})
		s.mu.Unlock()
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch verb {
		case "EHLO":
			if s.offerSTARTTLS && !isTLS {
				reply("250-stub")
				reply("250-STARTTLS")
			} else {
				reply("250-stub")
			}
			reply("250 AUTH PLAIN")
		case "STARTTLS":
			reply("220 go ahead")
			tc := tls.Server(raw, tlsCfg)
			if tc.HandshakeContext(context.Background()) != nil {
				return
			}
			conn, isTLS, r = tc, true, bufio.NewReader(tc)
		case "AUTH":
			reply(pick("AUTH", "235 ok"))
		case "MAIL", "RCPT":
			reply(pick(verb, "250 ok"))
		case "DATA":
			if v := pick("DATA", "354 go"); !strings.HasPrefix(v, "354") {
				reply(v)
				continue
			}
			reply("354 go")
			var body bytes.Buffer
			for {
				l, readErr := r.ReadString('\n')
				if readErr != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				body.WriteString(strings.TrimPrefix(l, "."))
			}
			s.mu.Lock()
			s.data = append(s.data, body.Bytes())
			s.mu.Unlock()
			reply(pick("END", "250 queued"))
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 unknown")
		}
	}
}

func (s *smtpStub) snapshot() (int, []stubCommand, [][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conns, append([]stubCommand(nil), s.commands...), append([][]byte(nil), s.data...)
}

func (s *smtpStub) count(verb string) int {
	_, cmds, _ := s.snapshot()
	n := 0
	for _, c := range cmds {
		if strings.HasPrefix(strings.ToUpper(c.line), verb) {
			n++
		}
	}
	return n
}

var stubNow = time.Date(2026, 10, 10, 9, 30, 0, 0, time.UTC)

// fixedRandom returns the same bytes on every call, so a test can render the
// message the sender will send.
type fixedRandom struct{}

func (fixedRandom) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(i + 7)
	}
	return len(p), nil
}

func newStubSender(t *testing.T, stub *smtpStub, roots *x509.CertPool) (Sender, *logBuffer) {
	t.Helper()
	logger, buf := testLogger()
	mode := SMTPTLSStartTLS
	if stub.implicit {
		mode = SMTPTLSImplicit
	}
	s, err := NewSMTPSender(SMTPOptions{
		Host: "127.0.0.1", Port: stub.port(), TLSMode: mode,
		Username: stubUser, Password: stubPassword,
		From: stubFrom, FromName: "Danny from aboutme.vn",
		RootCAs: roots, Logger: logger,
		Now: func() time.Time { return stubNow }, Random: fixedRandom{},
	})
	if err != nil {
		t.Fatalf("NewSMTPSender: %v", err)
	}
	return s, buf
}

func stubMessage() Message {
	return Message{Kind: KindVerify, To: stubTo, Subject: "Xác minh email subject-secret",
		TextBody: "text body-secret\n.leading dot", HTMLBody: "<p>html body-secret</p>"}
}

func TestSMTPSenderDeliversOverTLS(t *testing.T) {
	t.Parallel()
	roots, cert := testPKI(t)
	for _, implicit := range []bool{true, false} {
		stub := startStub(t, cert, func(s *smtpStub) { s.implicit = implicit })
		sender, _ := newStubSender(t, stub, roots)
		msg := stubMessage()
		res, err := sender.Send(context.Background(), msg)
		if err != nil || res.Outcome != SendAccepted {
			t.Fatalf("implicit=%v: Send = %v, %v; want accepted", implicit, res, err)
		}
		_, cmds, data := stub.snapshot()
		want, err := renderSMTPMessage(`"Danny from aboutme.vn" <danny@aboutme.vn>`, "aboutme.vn", msg, stubNow, fixedRandom{})
		if err != nil {
			t.Fatal(err)
		}
		if len(data) != 1 || !bytes.Equal(data[0], want) {
			t.Fatalf("implicit=%v: DATA = %q, want %q", implicit, data, want)
		}
		var sawMail, sawRcpt bool
		for _, c := range cmds {
			switch {
			case strings.HasPrefix(c.line, "AUTH"):
				wantAuth := "AUTH PLAIN " + base64.StdEncoding.EncodeToString([]byte("\x00"+stubUser+"\x00"+stubPassword))
				if !c.tls || c.line != wantAuth {
					t.Errorf("implicit=%v: AUTH line %q tls=%v", implicit, c.line, c.tls)
				}
			case strings.HasPrefix(c.line, "MAIL FROM:"):
				sawMail = c.line == "MAIL FROM:<"+stubFrom+">"
			case strings.HasPrefix(c.line, "RCPT TO:"):
				sawRcpt = c.line == "RCPT TO:<"+stubTo+">"
			}
		}
		if !sawMail || !sawRcpt {
			t.Errorf("implicit=%v: envelope commands %v", implicit, cmds)
		}
	}
}

func TestSMTPSenderOneConnectionPerMessage(t *testing.T) {
	t.Parallel()
	roots, cert := testPKI(t)
	stub := startStub(t, cert, func(s *smtpStub) { s.implicit = true })
	sender, _ := newStubSender(t, stub, roots)
	for i := 0; i < 2; i++ {
		if res, err := sender.Send(context.Background(), stubMessage()); err != nil || res.Outcome != SendAccepted {
			t.Fatalf("send %d = %v, %v", i, res, err)
		}
	}
	if conns, _, data := stub.snapshot(); conns != 2 || len(data) != 2 || stub.count("MAIL") != 2 {
		t.Errorf("conns=%d data=%d mail=%d, want 2 each", conns, len(data), stub.count("MAIL"))
	}
}

func TestSMTPSenderRejectsUntrustedCertificate(t *testing.T) {
	t.Parallel()
	_, cert := testPKI(t)
	for _, implicit := range []bool{true, false} {
		stub := startStub(t, cert, func(s *smtpStub) { s.implicit = implicit })
		sender, _ := newStubSender(t, stub, nil) // system roots do not hold the test CA
		res, err := sender.Send(context.Background(), stubMessage())
		if err != nil || res.Outcome != SendTemporaryFailure {
			t.Errorf("implicit=%v: Send = %v, %v; want temporary", implicit, res, err)
		}
		if stub.count("AUTH") != 0 || stub.count("MAIL") != 0 {
			t.Errorf("implicit=%v: credentials or envelope sent over an unverified connection", implicit)
		}
	}
}

func TestSMTPSenderNeverSendsPlainBeforeTLS(t *testing.T) {
	t.Parallel()
	roots, cert := testPKI(t)
	stub := startStub(t, cert, func(s *smtpStub) { s.offerSTARTTLS = false })
	sender, _ := newStubSender(t, stub, roots)
	res, err := sender.Send(context.Background(), stubMessage())
	if err != nil || res.Outcome != SendTemporaryFailure {
		t.Fatalf("Send = %v, %v; want temporary", res, err)
	}
	if stub.count("AUTH") != 0 || stub.count("MAIL") != 0 {
		t.Error("AUTH or MAIL sent on a connection without TLS")
	}
}

func TestSMTPSenderClassifiesReplies(t *testing.T) {
	t.Parallel()
	roots, cert := testPKI(t)
	tests := []struct {
		verb, reply string
		want        SendOutcome
	}{
		{"AUTH", "535 5.7.8 bad credentials", SendPermanentFailure},
		{"MAIL", "421 4.3.2 shutting down", SendTemporaryFailure},
		{"RCPT", "450 4.2.1 mailbox busy", SendTemporaryFailure},
		{"RCPT", "550 5.1.1 " + stubTo + " unknown", SendPermanentFailure},
		{"DATA", "554 5.3.4 no data", SendPermanentFailure},
		{"END", "451 4.3.0 try later", SendTemporaryFailure},
		{"END", "554 5.7.1 " + stubTo + " rejected", SendPermanentFailure},
		{"END", "251 2.0.0 queued", SendAccepted},
	}
	for _, tt := range tests {
		stub := startStub(t, cert, func(s *smtpStub) { s.replies[tt.verb] = tt.reply })
		sender, logs := newStubSender(t, stub, roots)
		res, err := sender.Send(context.Background(), stubMessage())
		if err != nil || res.Outcome != tt.want {
			t.Errorf("%s %q: Send = %v, %v; want %v", tt.verb, tt.reply, res, err, tt.want)
		}
		line := logs.String()
		if tt.want == SendAccepted {
			if line != "" {
				t.Errorf("%s %q: accepted send logged %q", tt.verb, tt.reply, line)
			}
			continue
		}
		code := tt.reply[:3]
		if !strings.Contains(line, "code="+code) || !strings.Contains(line, "outcome="+tt.want.String()) {
			t.Errorf("%s: log %q lacks code=%s", tt.verb, line, code)
		}
		for _, secret := range []string{stubTo, "secret", stubUser, stubPassword, stubBanner, tt.reply[4:]} {
			if strings.Contains(line, secret) {
				t.Errorf("%s: log %q leaks %q", tt.verb, line, secret)
			}
		}
	}
}

func TestSMTPSenderTimeoutIsTemporary(t *testing.T) {
	t.Parallel()
	roots, cert := testPKI(t)
	stub := startStub(t, cert, func(s *smtpStub) { s.silent = true })
	sender, logs := newStubSender(t, stub, roots)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	res, err := sender.Send(ctx, stubMessage())
	if err != nil || res.Outcome != SendTemporaryFailure {
		t.Fatalf("Send = %v, %v; want temporary", res, err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Send took %v, want it bounded by the context deadline", elapsed)
	}
	if strings.Contains(logs.String(), "code=") {
		t.Errorf("timeout log %q carries a reply code", logs.String())
	}
}

func TestNewSMTPSenderRejectsInvalidOptions(t *testing.T) {
	t.Parallel()
	logger, _ := testLogger()
	valid := func() SMTPOptions {
		return SMTPOptions{Host: "smtp.example.com", Port: 2465, TLSMode: SMTPTLSImplicit,
			Username: "u", Password: "p", From: stubFrom, Logger: logger}
	}
	if _, err := NewSMTPSender(valid()); err != nil {
		t.Fatalf("valid options rejected: %v", err)
	}
	for name, mut := range map[string]func(*SMTPOptions){
		"empty host":         func(o *SMTPOptions) { o.Host = "" },
		"host with port":     func(o *SMTPOptions) { o.Host = "smtp.example.com:2465" },
		"zero port":          func(o *SMTPOptions) { o.Port = 0 },
		"unknown tls mode":   func(o *SMTPOptions) { o.TLSMode = "none" },
		"empty username":     func(o *SMTPOptions) { o.Username = "" },
		"password with CRLF": func(o *SMTPOptions) { o.Password = "p\r\n" },
		"named from":         func(o *SMTPOptions) { o.From = "Danny <" + stubFrom + ">" },
		"from name control":  func(o *SMTPOptions) { o.FromName = "a\nb" },
		"nil logger":         func(o *SMTPOptions) { o.Logger = nil },
	} {
		opts := valid()
		mut(&opts)
		if _, err := NewSMTPSender(opts); err == nil {
			t.Errorf("%s: NewSMTPSender error = nil", name)
		}
	}
}
