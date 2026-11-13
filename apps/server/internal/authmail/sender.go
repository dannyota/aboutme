package authmail

import (
	"context"
	"errors"
)

type Message struct {
	Kind     Kind   `json:"kind"`
	To       string `json:"to"`
	Subject  string `json:"subject"`
	TextBody string `json:"text_body"`
	HTMLBody string `json:"html_body"`
}

type SendOutcome uint8

const (
	SendAccepted SendOutcome = iota
	SendTemporaryFailure
	SendPermanentFailure
)

type SendResult struct {
	Outcome SendOutcome
}

type Sender interface {
	Send(context.Context, Message) (SendResult, error)
}

var (
	ErrWorker = errors.New("authmail: invalid worker options")
	ErrSES    = errors.New("authmail: invalid ses sender options")
)
