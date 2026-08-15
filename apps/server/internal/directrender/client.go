// Package directrender calls the local renderer for a public resume response.
package directrender

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	publicRenderRequestMaxBytes  int64 = 532_480
	publicRenderResponseMaxBytes int64 = 2_097_152
	publicRenderDeadline               = 5 * time.Second
)

var errRenderRequestTooLarge = errors.New("direct render request exceeds limit")
var errInvalidRenderResponse = errors.New("invalid direct render response")

type limitedBuffer struct {
	bytes.Buffer
	limit int64
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	if int64(b.Len()+len(value)) > b.limit {
		return 0, errRenderRequestTooLarge
	}
	return b.Buffer.Write(value)
}

type renderOutcome struct {
	response *http.Response
	err      error
}

// Render returns a bounded, validated HTML representation for request.
func (c *Client) Render(ctx context.Context, request PublicRenderRequest) (Result, error) {
	if c == nil || c.http == nil || c.origin.value == "" {
		return Result{}, renderUnavailable(errors.New("direct render client is not configured"))
	}
	body, err := encodeRequest(request)
	if err != nil {
		return Result{}, renderUnavailable(err)
	}
	renderCtx, cancel := context.WithTimeout(ctx, publicRenderDeadline)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(renderCtx, http.MethodPost, c.origin.value+"/internal-render/public", bytes.NewReader(body))
	if err != nil {
		return Result{}, renderUnavailable(err)
	}
	httpRequest.Header.Set("Content-Type", "application/json; charset=utf-8")
	done := make(chan renderOutcome, 1)
	go func() {
		//nolint:bodyclose // Render joins the request and readResponse closes every returned body.
		response, doErr := c.http.Do(httpRequest)
		done <- renderOutcome{response: response, err: doErr}
	}()

	var outcome renderOutcome
	select {
	case outcome = <-done:
	case <-renderCtx.Done():
		// The direct call owns no work after this function returns: wait for the
		// transport to observe cancellation, then close a late response.
		outcome = <-done
		return Result{}, renderUnavailable(errors.Join(renderCtx.Err(), closeResponse(outcome.response)))
	}
	if outcome.err != nil {
		return Result{}, renderUnavailable(errors.Join(outcome.err, closeResponse(outcome.response)))
	}
	if outcome.response == nil || outcome.response.Body == nil {
		return Result{}, renderUnavailable(errInvalidRenderResponse)
	}
	return readResponse(outcome.response)
}

func encodeRequest(request PublicRenderRequest) ([]byte, error) {
	buffer := &limitedBuffer{limit: publicRenderRequestMaxBytes}
	if err := json.NewEncoder(buffer).Encode(request); err != nil {
		return nil, err
	}
	body := buffer.Bytes()
	if len(body) == 0 || body[len(body)-1] != '\n' {
		return nil, errRenderRequestTooLarge
	}
	return append([]byte{}, body[:len(body)-1]...), nil
}

func readResponse(response *http.Response) (Result, error) {
	if response.StatusCode != http.StatusOK {
		return Result{}, renderUnavailable(errors.Join(
			&RenderStatusError{Status: response.StatusCode},
			closeResponse(response),
		))
	}
	contentTypes := response.Header.Values("Content-Type")
	if len(contentTypes) != 1 || contentTypes[0] != "text/html; charset=utf-8" {
		return Result{}, renderUnavailable(errors.Join(errInvalidRenderResponse, closeResponse(response)))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, publicRenderResponseMaxBytes+1))
	closeErr := response.Body.Close()
	if err != nil {
		return Result{}, renderUnavailable(err)
	}
	if closeErr != nil {
		return Result{}, renderUnavailable(closeErr)
	}
	if int64(len(body)) > publicRenderResponseMaxBytes {
		return Result{}, renderUnavailable(&RenderResponseTooLargeError{Limit: publicRenderResponseMaxBytes})
	}
	return Result{HTML: body}, nil
}

func closeResponse(response *http.Response) error {
	if response == nil || response.Body == nil {
		return nil
	}
	return response.Body.Close()
}

func renderUnavailable(cause error) error {
	return fmt.Errorf("%w: %w", ErrRenderUnavailable, cause)
}
