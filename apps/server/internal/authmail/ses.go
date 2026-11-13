package authmail

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/mail"
	"unicode"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sesv2types "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/smithy-go"
)

const sesRegion = "ap-southeast-1"

const sesCharset = "UTF-8"

const maxFromNameRunes = 64

const maxErrorCodeLen = 64

type SESClient interface {
	SendEmail(context.Context, *sesv2.SendEmailInput, ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error)
}

type SESOptions struct {
	Region           string
	From             string
	FromName         string
	ConfigurationSet string
	Client           SESClient
	Logger           *slog.Logger
}

func NewSESClient(ctx context.Context, region string) (*sesv2.Client, error) {
	if region != sesRegion {
		return nil, ErrSES
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, ErrSES
	}
	return sesv2.NewFromConfig(cfg, func(o *sesv2.Options) {
		o.RetryMaxAttempts = 0
	}), nil
}

func NewSESSender(opts SESOptions) (Sender, error) {
	if opts.Region != sesRegion {
		return nil, ErrSES
	}
	if opts.From == "" || opts.ConfigurationSet == "" {
		return nil, ErrSES
	}
	if opts.Client == nil || opts.Logger == nil {
		return nil, ErrSES
	}
	if !validFromName(opts.FromName) {
		return nil, ErrSES
	}
	from := opts.From
	if opts.FromName != "" {
		// net/mail's Address.String quotes the name and RFC 2047-encodes non-ASCII text.
		from = (&mail.Address{Name: opts.FromName, Address: opts.From}).String()
	}
	return &sesSender{from: from, configurationSet: opts.ConfigurationSet, client: opts.Client, logger: opts.Logger}, nil
}

type sesSender struct {
	from             string
	configurationSet string
	client           SESClient
	logger           *slog.Logger
}

func (s *sesSender) Send(ctx context.Context, msg Message) (SendResult, error) {
	input := &sesv2.SendEmailInput{
		FromEmailAddress:     aws.String(s.from),
		ConfigurationSetName: aws.String(s.configurationSet),
		Destination: &sesv2types.Destination{
			ToAddresses: []string{msg.To},
		},
		Content: &sesv2types.EmailContent{
			Simple: &sesv2types.Message{
				Subject: &sesv2types.Content{Data: aws.String(msg.Subject), Charset: aws.String(sesCharset)},
				Body: &sesv2types.Body{
					Text: &sesv2types.Content{Data: aws.String(msg.TextBody), Charset: aws.String(sesCharset)},
					Html: &sesv2types.Content{Data: aws.String(msg.HTMLBody), Charset: aws.String(sesCharset)},
				},
			},
		},
	}

	if _, err := s.client.SendEmail(ctx, input); err != nil {
		outcome := classifySendError(err)
		attrs := []any{"outcome", outcome.String()}
		if code := sesErrorCode(err); code != "" {
			attrs = append(attrs, "code", code)
		}
		s.logger.Warn("authmail: ses send failed", attrs...)
		return SendResult{Outcome: outcome}, nil
	}
	return SendResult{Outcome: SendAccepted}, nil
}

func classifySendError(err error) SendOutcome {
	if err == nil {
		return SendAccepted
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return SendTemporaryFailure
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorFault() {
		case smithy.FaultServer:
			return SendTemporaryFailure
		case smithy.FaultClient:
			switch apiErr.ErrorCode() {
			case "TooManyRequestsException", "LimitExceededException":
				return SendTemporaryFailure
			default:
				return SendPermanentFailure
			}
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return SendTemporaryFailure
	}
	var opErr *smithy.OperationError
	if errors.As(err, &opErr) {
		return SendTemporaryFailure
	}
	return SendTemporaryFailure
}

func sesErrorCode(err error) string {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return ""
	}
	code := apiErr.ErrorCode()
	if code == "" || len(code) > maxErrorCodeLen {
		return ""
	}
	for _, r := range code {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return ""
		}
	}
	return code
}

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
