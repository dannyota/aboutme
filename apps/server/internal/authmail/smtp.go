package authmail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type SMTPTLSMode string

const (
	SMTPTLSImplicit SMTPTLSMode = "implicit"
	SMTPTLSStartTLS SMTPTLSMode = "starttls"
)

const smtpMaxTimeout = 30 * time.Second

const maxSMTPCredentialBytes = 256

var ErrSMTP = errors.New("authmail: invalid smtp sender options")

var errSMTPRender = errors.New("authmail: smtp message render failed")

var errNoSTARTTLS = errors.New("authmail: smtp server did not offer STARTTLS")

type SMTPOptions struct {
	Host     string
	Port     int
	TLSMode  SMTPTLSMode
	Username string
	Password string
	From     string
	FromName string
	RootCAs  *x509.CertPool
	Logger   *slog.Logger
	Now      func() time.Time
	Random   io.Reader
}

type smtpSender struct {
	addr       string
	host       string
	mode       SMTPTLSMode
	username   string
	password   string
	from       string
	fromHeader string
	heloName   string
	rootCAs    *x509.CertPool
	logger     *slog.Logger
	now        func() time.Time
	random     io.Reader
}

func NewSMTPSender(opts SMTPOptions) (Sender, error) {
	if opts.Host == "" || strings.ContainsAny(opts.Host, " \r\n/:@") {
		return nil, ErrSMTP
	}
	if opts.Port < 1 || opts.Port > 65535 {
		return nil, ErrSMTP
	}
	if opts.TLSMode != SMTPTLSImplicit && opts.TLSMode != SMTPTLSStartTLS {
		return nil, ErrSMTP
	}
	if !validSMTPCredential(opts.Username) || !validSMTPCredential(opts.Password) {
		return nil, ErrSMTP
	}
	if opts.Logger == nil || !validFromName(opts.FromName) {
		return nil, ErrSMTP
	}
	parsed, err := mail.ParseAddress(opts.From)
	if err != nil || parsed.Name != "" || parsed.Address != opts.From {
		return nil, ErrSMTP
	}
	at := strings.LastIndexByte(opts.From, '@')
	if at <= 0 || at == len(opts.From)-1 {
		return nil, ErrSMTP
	}
	now, random := opts.Now, opts.Random
	if now == nil {
		now = time.Now
	}
	if random == nil {
		random = rand.Reader
	}
	return &smtpSender{
		addr:       net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port)),
		host:       opts.Host,
		mode:       opts.TLSMode,
		username:   opts.Username,
		password:   opts.Password,
		from:       opts.From,
		fromHeader: (&mail.Address{Name: opts.FromName, Address: opts.From}).String(),
		heloName:   opts.From[at+1:],
		rootCAs:    opts.RootCAs,
		logger:     opts.Logger,
		now:        now,
		random:     random,
	}, nil
}

func (s *smtpSender) Send(ctx context.Context, msg Message) (SendResult, error) {
	data, renderErr := renderSMTPMessage(s.fromHeader, s.heloName, msg, s.now(), s.random)
	if renderErr != nil {
		s.logger.Warn("authmail: smtp send failed", "outcome", SendTemporaryFailure.String())
		return SendResult{Outcome: SendTemporaryFailure}, errSMTPRender
	}
	if err := s.deliver(ctx, msg.To, data); err != nil {
		outcome, code := classifySMTPError(err)
		attrs := []any{"outcome", outcome.String()}
		if code != 0 {
			attrs = append(attrs, "code", code)
		}
		s.logger.Warn("authmail: smtp send failed", attrs...)
		return SendResult{Outcome: outcome}, nil
	}
	return SendResult{Outcome: SendAccepted}, nil
}

func (s *smtpSender) deliver(ctx context.Context, to string, data []byte) error {
	deadline := time.Now().Add(smtpMaxTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialer := net.Dialer{Deadline: deadline}
	conn, err := dialer.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return err
	}
	defer closeQuietly(conn)
	if err = conn.SetDeadline(deadline); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() {
		if deadlineErr := conn.SetDeadline(time.Unix(1, 0)); deadlineErr != nil {
			return
		}
	})
	defer stop()

	tlsConfig := &tls.Config{ServerName: s.host, RootCAs: s.rootCAs, MinVersion: tls.VersionTLS12}
	transport := conn
	if s.mode == SMTPTLSImplicit {
		tlsConn := tls.Client(conn, tlsConfig)
		if err = tlsConn.HandshakeContext(ctx); err != nil {
			return err
		}
		transport = tlsConn
	}
	client, err := smtp.NewClient(transport, s.host)
	if err != nil {
		return err
	}
	if err = client.Hello(s.heloName); err != nil {
		return err
	}
	if s.mode == SMTPTLSStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errNoSTARTTLS
		}
		if err = client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if _, ok := client.TLSConnectionState(); !ok {
		return errNoSTARTTLS
	}
	if err = client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
		return err
	}
	if err = client.Mail(s.from); err != nil {
		return err
	}
	if err = client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = w.Write(data); err != nil {
		return err
	}
	if err = w.Close(); err != nil && !isSMTPReply2xx(err) {
		return err
	}
	quitQuietly(client)
	return nil
}

// net/smtp expects exactly 250 after DATA, but any 2xx reply means the server accepted the message.
func isSMTPReply2xx(err error) bool {
	var reply *textproto.Error
	return errors.As(err, &reply) && reply.Code >= 200 && reply.Code <= 299
}

func classifySMTPError(err error) (SendOutcome, int) {
	var reply *textproto.Error
	if !errors.As(err, &reply) || reply.Code < 100 || reply.Code > 599 {
		return SendTemporaryFailure, 0
	}
	switch reply.Code / 100 {
	case 5:
		return SendPermanentFailure, reply.Code
	default:
		return SendTemporaryFailure, reply.Code
	}
}

func renderSMTPMessage(fromHeader, domain string, msg Message, now time.Time, random io.Reader) ([]byte, error) {
	var token [24]byte
	if _, err := io.ReadFull(random, token[:]); err != nil {
		return nil, err
	}
	to := (&mail.Address{Address: msg.To}).String()
	if strings.ContainsAny(to+msg.Subject, "\r\n") {
		return nil, errors.New("authmail: header contains a line break")
	}
	var buf bytes.Buffer
	body := multipart.NewWriter(&buf)
	if err := body.SetBoundary(hex.EncodeToString(token[8:])); err != nil {
		return nil, err
	}
	headers := []string{
		"From: " + fromHeader,
		"To: " + to,
		"Subject: " + mime.QEncoding.Encode("utf-8", msg.Subject),
		"Date: " + now.UTC().Format(time.RFC1123Z),
		fmt.Sprintf("Message-ID: <%s@%s>", hex.EncodeToString(token[:8]), domain),
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=" + body.Boundary(),
	}
	var out bytes.Buffer
	out.WriteString(strings.Join(headers, "\r\n") + "\r\n\r\n")
	for _, part := range []struct{ contentType, text string }{
		{"text/plain; charset=UTF-8", msg.TextBody},
		{"text/html; charset=UTF-8", msg.HTMLBody},
	} {
		pw, err := body.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.contentType},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		qp := quotedprintable.NewWriter(pw)
		if _, err = qp.Write([]byte(part.text)); err != nil {
			return nil, err
		}
		if err = qp.Close(); err != nil {
			return nil, err
		}
	}
	if err := body.Close(); err != nil {
		return nil, err
	}
	out.Write(buf.Bytes())
	return out.Bytes(), nil
}

func validSMTPCredential(v string) bool {
	if v == "" || len(v) > maxSMTPCredentialBytes || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func quitQuietly(c *smtp.Client) {
	if err := c.Quit(); err != nil {
		return
	}
}

func closeQuietly(c io.Closer) {
	if err := c.Close(); err != nil {
		return
	}
}
