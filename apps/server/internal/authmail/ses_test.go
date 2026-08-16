package authmail

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sesv2types "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/smithy-go"
)

// fakeSESClient records every SendEmail call and returns the configured error
// or success. It never constructs a real AWS client.
type fakeSESClient struct {
	mu    sync.Mutex
	calls []*sesv2.SendEmailInput
	err   error
}

func (f *fakeSESClient) SendEmail(_ context.Context, params *sesv2.SendEmailInput, _ ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, params)
	if f.err != nil {
		return nil, f.err
	}
	return &sesv2.SendEmailOutput{}, nil
}

func (f *fakeSESClient) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// logBuffer is a concurrency-safe in-memory log sink.
type logBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func testLogger() (*slog.Logger, *logBuffer) {
	buf := &logBuffer{}
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

func newTestSESSender(t *testing.T, client SESClient) (Sender, *logBuffer) {
	t.Helper()
	logger, buf := testLogger()
	s, err := NewSESSender(SESOptions{
		Region:           sesRegion,
		From:             "no-reply@aboutme.vn",
		ConfigurationSet: "aboutme-auth",
		Client:           client,
		Logger:           logger,
	})
	if err != nil {
		t.Fatalf("NewSESSender: %v", err)
	}
	return s, buf
}

func TestNewSESSenderRejectsBadOptions(t *testing.T) {
	good := SESOptions{
		Region:           sesRegion,
		From:             "no-reply@aboutme.vn",
		ConfigurationSet: "aboutme-auth",
		Client:           &fakeSESClient{},
		Logger:           slog.New(slog.NewTextHandler(&logBuffer{}, nil)),
	}
	cases := []struct {
		name string
		mut  func(*SESOptions)
	}{
		{"wrong region", func(o *SESOptions) { o.Region = "us-east-1" }},
		{"empty from", func(o *SESOptions) { o.From = "" }},
		{"empty configuration set", func(o *SESOptions) { o.ConfigurationSet = "" }},
		{"nil client", func(o *SESOptions) { o.Client = nil }},
		{"nil logger", func(o *SESOptions) { o.Logger = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := good
			tc.mut(&opts)
			if _, err := NewSESSender(opts); !errors.Is(err, ErrSES) {
				t.Fatalf("err = %v, want ErrSES", err)
			}
		})
	}
}

func TestNewSESClientRejectsWrongRegion(t *testing.T) {
	// NewSESClient constructs a real client, so it must never be called with a
	// non-production region and never be called in tests with the real region.
	if _, err := NewSESClient(context.Background(), "us-east-1"); !errors.Is(err, ErrSES) {
		t.Fatalf("err = %v, want ErrSES", err)
	}
}

func TestSESSendSetsExactInputFields(t *testing.T) {
	fake := &fakeSESClient{}
	s, _ := newTestSESSender(t, fake)

	msg := Message{
		Kind:     KindVerify,
		To:       "alice@example.com",
		Subject:  "Verify your email",
		TextBody: "Confirm your email address by opening this link:\nhttps://aboutme.vn/verify-email#token=t",
		HTMLBody: "<p>Confirm your email address by opening this link:</p>",
	}
	res, err := s.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Outcome != SendAccepted {
		t.Fatalf("outcome = %v, want accepted", res.Outcome)
	}
	if fake.callCount() != 1 {
		t.Fatalf("calls = %d, want exactly 1 (no SDK retry)", fake.callCount())
	}
	in := fake.calls[0]
	if got := derefS(in.FromEmailAddress); got != "no-reply@aboutme.vn" {
		t.Errorf("FromEmailAddress = %q, want no-reply@aboutme.vn", got)
	}
	if got := derefS(in.ConfigurationSetName); got != "aboutme-auth" {
		t.Errorf("ConfigurationSetName = %q, want aboutme-auth", got)
	}
	if in.Destination == nil || len(in.Destination.ToAddresses) != 1 || in.Destination.ToAddresses[0] != "alice@example.com" {
		t.Errorf("Destination.ToAddresses = %v, want [alice@example.com]", in.Destination.ToAddresses)
	}
	if in.Content == nil || in.Content.Simple == nil {
		t.Fatal("Content.Simple is nil")
	}
	if got := derefS(in.Content.Simple.Subject.Data); got != msg.Subject {
		t.Errorf("subject = %q, want %q", got, msg.Subject)
	}
	if got := derefS(in.Content.Simple.Subject.Charset); got != "UTF-8" {
		t.Errorf("subject charset = %q, want UTF-8", got)
	}
	if in.Content.Simple.Body == nil {
		t.Fatal("Body is nil")
	}
	if got := derefS(in.Content.Simple.Body.Text.Data); got != msg.TextBody {
		t.Errorf("text body = %q, want %q", got, msg.TextBody)
	}
	if got := derefS(in.Content.Simple.Body.Html.Data); got != msg.HTMLBody {
		t.Errorf("html body = %q, want %q", got, msg.HTMLBody)
	}
}

func TestSESSendNoRecipientBodyOrRequestIDInLogs(t *testing.T) {
	fake := &fakeSESClient{err: &sesv2types.MessageRejected{}}
	s, buf := newTestSESSender(t, fake)

	msg := Message{
		Kind:     KindReset,
		To:       "secret-recipient@example.com",
		Subject:  "SECRET SUBJECT",
		TextBody: "SECRET TEXT",
		HTMLBody: "SECRET HTML",
	}
	res, err := s.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Outcome != SendPermanentFailure {
		t.Fatalf("outcome = %v, want permanent", res.Outcome)
	}
	logged := buf.String()
	for _, secret := range []string{"secret-recipient@example.com", "SECRET SUBJECT", "SECRET TEXT", "SECRET HTML", "MessageRejected"} {
		if strings.Contains(logged, secret) {
			t.Errorf("log leaks %q: %s", secret, logged)
		}
	}
	if !strings.Contains(logged, "permanent") {
		t.Errorf("log should name the closed outcome, got: %s", logged)
	}
}

func TestClassifySendError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want SendOutcome
	}{
		{"nil", nil, SendAccepted},
		{"deadline exceeded", context.DeadlineExceeded, SendTemporaryFailure},
		{"canceled", context.Canceled, SendTemporaryFailure},
		{"too many requests 429", &sesv2types.TooManyRequestsException{}, SendTemporaryFailure},
		{"limit exceeded throttling", &sesv2types.LimitExceededException{}, SendTemporaryFailure},
		{"server 5xx", &sesv2types.InternalServiceErrorException{}, SendTemporaryFailure},
		{"concurrent server fault", &sesv2types.ConcurrentModificationException{}, SendTemporaryFailure},
		{"message rejected", &sesv2types.MessageRejected{}, SendPermanentFailure},
		{"bad request validation", &sesv2types.BadRequestException{}, SendPermanentFailure},
		{"mail from unverified", &sesv2types.MailFromDomainNotVerifiedException{}, SendPermanentFailure},
		{"account suspended", &sesv2types.AccountSuspendedException{}, SendPermanentFailure},
		{"not found", &sesv2types.NotFoundException{}, SendPermanentFailure},
		{"sending paused", &sesv2types.SendingPausedException{}, SendPermanentFailure},
		{"already exists", &sesv2types.AlreadyExistsException{}, SendPermanentFailure},
		{"transport operation error", &smithy.OperationError{Err: &net.OpError{Err: errors.New("dial")}}, SendTemporaryFailure},
		{"raw net error", &net.OpError{Err: errors.New("timeout")}, SendTemporaryFailure},
		{"unknown ambiguous", errors.New("something went sideways"), SendTemporaryFailure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifySendError(tc.err); got != tc.want {
				t.Fatalf("classifySendError = %v, want %v", got, tc.want)
			}
		})
	}
}

func derefS(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
