package authmail

import (
	"context"
	"errors"
	"log/slog"
	"net"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sesv2types "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/smithy-go"
)

// sesRegion is the exact SES region (D7).
const sesRegion = "ap-southeast-1"

// sesCharset is the charset for every text/HTML content part.
const sesCharset = "UTF-8"

// SESClient is the minimal SendEmail surface production SES v2 provides; tests
// inject a fake so no AWS client is ever constructed in tests.
type SESClient interface {
	SendEmail(context.Context, *sesv2.SendEmailInput, ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error)
}

// SESOptions configures the SES sender. Region must be exactly ap-southeast-1,
// From must be a configured verified sender, ConfigurationSet must be set, and
// Client is injected so production wiring can pass a retry-disabled client
// while tests pass a fake.
type SESOptions struct {
	Region           string
	From             string
	ConfigurationSet string
	Client           SESClient
	Logger           *slog.Logger
}

// NewSESClient constructs a real SES v2 client for the exact D7 region with SDK
// retries disabled: the Worker owns retry timing, so the SDK must never retry a
// SendEmail itself. It is only ever called by production wiring (T09), never by
// tests or capture mode.
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

// NewSESSender validates the exact D7 configuration and returns a Sender that
// owns one SendEmail call per Message. It never logs a recipient, body, AWS
// request ID, or raw SDK error; every failure collapses to a closed outcome.
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
	return &sesSender{from: opts.From, configurationSet: opts.ConfigurationSet, client: opts.Client, logger: opts.Logger}, nil
}

type sesSender struct {
	from             string
	configurationSet string
	client           SESClient
	logger           *slog.Logger
}

// Send delivers msg through SESv2 and classifies the outcome.
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
		// Generic, secret-free log: no recipient, body, request ID, or SDK text.
		s.logger.Warn("authmail: ses send failed", "outcome", outcome.String())
		return SendResult{Outcome: outcome}, nil
	}
	return SendResult{Outcome: SendAccepted}, nil
}

// classifySendError maps one SDK error to the closed D7 outcome taxonomy.
//   - Temporary: deadline/cancel, any transport/ambiguous error, 429
//     (TooManyRequestsException), throttling (LimitExceededException), and every
//     server fault (5xx).
//   - Permanent: every other closed client fault (validation, rejected content,
//     unverified or suspended sender, and other 4xx).
//   - Unknown non-API errors are ambiguous and therefore temporary: a duplicate
//     delivery is harmless because token authority and single use make it so.
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
