package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	signerAccessSentinel = "SIGNER-" + "AKID-6c914f8d3a27"
	signerSecretSentinel = "SIGNER-" + "SECRET-b510e73f29c4"
)

type failingHTTPSignerV4 struct {
	putCalls    atomic.Int64
	getCalls    atomic.Int64
	deleteCalls atomic.Int64
}

func (s *failingHTTPSignerV4) SignHTTP(
	_ context.Context,
	_ aws.Credentials,
	request *http.Request,
	_ string,
	_ string,
	_ string,
	_ time.Time,
	_ ...func(*v4.SignerOptions),
) error {
	if request.Method == http.MethodHead {
		return nil
	}
	switch request.Method {
	case http.MethodPut:
		s.putCalls.Add(1)
	case http.MethodGet:
		s.getCalls.Add(1)
	case http.MethodDelete:
		s.deleteCalls.Add(1)
	}
	return errors.New("signer failure: " + signerAccessSentinel + " " + signerSecretSentinel)
}

func TestS3SignerErrorsNeverLeak(t *testing.T) {
	var headRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("unexpected request reached service: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		headRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	awsCfg := aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider(signerAccessSentinel, signerSecretSentinel, ""),
	}
	signer := &failingHTTPSignerV4{}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(server.URL)
		options.UsePathStyle = true
		options.HTTPSignerV4 = signer
	})
	backend := &s3Backend{client: client, bucket: "stub-bucket"}
	const key = "resumes/x/signer.jpg"

	operations := []struct {
		name        string
		signerCalls *atomic.Int64
		run         func(*testing.T) error
	}{
		{
			name:        "put",
			signerCalls: &signer.putCalls,
			run: func(t *testing.T) error {
				outcome, err := backend.Put(context.Background(), key, "image/jpeg", bytes.NewReader([]byte("x")), 1)
				if outcome != PutUnknown {
					t.Errorf("Put outcome = %d, want PutUnknown", outcome)
				}
				return err
			},
		},
		{
			name:        "get",
			signerCalls: &signer.getCalls,
			run: func(t *testing.T) error {
				body, _, err := backend.Get(context.Background(), key)
				if body != nil {
					if closeErr := body.Close(); closeErr != nil {
						t.Errorf("close unexpected Get body: %v", closeErr)
					}
					t.Error("Get returned a body after signer failure")
				}
				return err
			},
		},
		{
			name:        "delete",
			signerCalls: &signer.deleteCalls,
			run: func(t *testing.T) error {
				before := headRequests.Load()
				err := backend.Delete(context.Background(), key)
				if got := headRequests.Load() - before; got != 1 {
					t.Errorf("HeadObject requests before DeleteObject signing = %d, want 1", got)
				}
				return err
			},
		},
	}

	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			before := operation.signerCalls.Load()
			err := operation.run(t)
			if err == nil {
				t.Fatal("operation returned nil error; signer failure was not exercised")
			}
			if got := operation.signerCalls.Load() - before; got == 0 {
				t.Fatal("operation did not reach the failing signer")
			}
			text := fmt.Sprintf("%+v", err)
			if strings.Contains(text, signerAccessSentinel) || strings.Contains(text, signerSecretSentinel) {
				t.Errorf("returned error leaks a signer credential sentinel: %s", text)
			}
		})
	}
}
