package authmail

import (
	"context"
	"errors"
	"unicode"
	"unicode/utf8"
)

const maxFromNameRunes = 64

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

var ErrWorker = errors.New("authmail: invalid worker options")

func (o SendOutcome) String() string {
	switch o {
	case SendAccepted:
		return "accepted"
	case SendTemporaryFailure:
		return "temporary"
	case SendPermanentFailure:
		return "permanent"
	default:
		return "unknown"
	}
}

func validFromName(name string) bool {
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxFromNameRunes {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
