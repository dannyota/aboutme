package authmail

import (
	"context"
	"errors"
)

// Message is the closed, sender-independent email a Worker hands to a Sender.
// Subjects and body templates are fixed code (D7); To comes from the sealed
// payload, never from user input, and no field carries a raw token, digest, or
// key material.
type Message struct {
	Kind     Kind   `json:"kind"`
	To       string `json:"to"`
	Subject  string `json:"subject"`
	TextBody string `json:"text_body"`
	HTMLBody string `json:"html_body"`
}

// SendOutcome classifies one send attempt (D7). Accepted marks the job sent;
// PermanentFailure marks it terminal; TemporaryFailure is every ambiguous
// result (timeout, transport uncertainty, 429/throttling, server 5xx) and may
// duplicate delivery, which token single-use makes harmless.
type SendOutcome uint8

// SendOutcome values a Sender returns to classify one attempt.
const (
	SendAccepted SendOutcome = iota
	SendTemporaryFailure
	SendPermanentFailure
)

// SendResult is the closed outcome of one Send call.
type SendResult struct {
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
	ErrWorker = errors.New("authmail: invalid worker options")
	ErrSES    = errors.New("authmail: invalid ses sender options")
)
