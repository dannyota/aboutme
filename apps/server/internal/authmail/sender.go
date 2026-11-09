package authmail

import (
	"context"
	"errors"
)

// Message is the closed, sender-independent email a Worker hands to a Sender.
// Subjects and body templates are fixed code; To comes from the sealed
// payload, never from user input, and no field carries a raw token, digest, or
// key material.
type Message struct {
	// Kind is the closed email kind the templates were chosen for.
	Kind Kind `json:"kind"`
	// To is the recipient from the sealed payload.
	To string `json:"to"`
	// Subject is the fixed subject for Kind.
	Subject string `json:"subject"`
	// TextBody is the rendered plain-text body.
	TextBody string `json:"text_body"`
	// HTMLBody is the rendered HTML body.
	HTMLBody string `json:"html_body"`
}

// SendOutcome classifies one send attempt. Accepted marks the job sent;
// PermanentFailure marks it terminal; TemporaryFailure is every ambiguous
// result (timeout, transport uncertainty, 429/throttling, server 5xx) and may
// duplicate delivery, which token single-use makes harmless.
type SendOutcome uint8

// SendOutcome values a Sender returns to classify one attempt.
const (
	// SendAccepted means the provider accepted the message.
	SendAccepted SendOutcome = iota
	// SendTemporaryFailure is any ambiguous result; the job may retry.
	SendTemporaryFailure
	// SendPermanentFailure is a definite rejection; the job becomes
	// terminal.
	SendPermanentFailure
)

// SendResult is the closed outcome of one Send call.
type SendResult struct {
	// Outcome classifies the attempt.
	Outcome SendOutcome
}

// Sender delivers exactly one Message. Implementations classify the attempt
// themselves and return a closed SendResult; a returned non-nil error is
// treated as an ambiguous temporary failure by the Worker. Errors never carry
// a recipient, body, raw SDK error, or AWS request ID.
type Sender interface {
	Send(context.Context, Message) (SendResult, error)
}

// Closed sentinels for worker/sender construction. None carries decrypted
// data, a raw SDK error, or a request ID.
var (
	// ErrWorker rejects invalid worker construction options.
	ErrWorker = errors.New("authmail: invalid worker options")
	// ErrSES rejects invalid SES sender options.
	ErrSES = errors.New("authmail: invalid ses sender options")
)
