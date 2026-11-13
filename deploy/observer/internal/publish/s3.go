package publish

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"danny.vn/aboutme/deploy/observer/internal/platform"
	"danny.vn/aboutme/deploy/observer/internal/verify"
)

const documentKey = ".well-known/deployment.json"

const verifiedPrefix = "verified/"

const jsonContentType = "application/json"

const maxCacheEntryBytes = 64 << 10

const documentCacheControl = "public, max-age=30"

func NewClient(cfg aws.Config, endpoint string) *s3.Client {
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		}
	})
}

type Publisher struct {
	client *s3.Client
	bucket string
}

func NewPublisher(client *s3.Client, bucket string) *Publisher {
	return &Publisher{client: client, bucket: bucket}
}

func (p *Publisher) Put(ctx context.Context, body []byte) error {
	_, err := p.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(p.bucket),
		Key:          aws.String(documentKey),
		Body:         bytes.NewReader(body),
		ContentType:  aws.String(jsonContentType),
		CacheControl: aws.String(documentCacheControl),
	})
	if err != nil {
		return fmt.Errorf("publish: put %s: %w", documentKey, err)
	}
	return nil
}

type Cache struct {
	client *s3.Client
	bucket string
}

func NewCache(client *s3.Client, bucket string) *Cache {
	return &Cache{client: client, bucket: bucket}
}

func (c *Cache) Get(ctx context.Context, digest string) (verify.Evidence, bool, error) {
	if !platform.ValidDigest(digest) {
		return verify.Evidence{}, false, fmt.Errorf("publish: invalid digest %q", digest)
	}
	key := verifiedKey(digest)
	out, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return verify.Evidence{}, false, nil
		}
		return verify.Evidence{}, false, fmt.Errorf("publish: get %s: %w", key, err)
	}
	defer func() {
		_ = out.Body.Close() //nolint:errcheck // best-effort close once the body below is fully decoded
	}()
	var ev verify.Evidence
	if err := json.NewDecoder(io.LimitReader(out.Body, maxCacheEntryBytes)).Decode(&ev); err != nil {
		return verify.Evidence{}, false, fmt.Errorf("publish: decode %s: %w", key, err)
	}
	return ev, true, nil
}

func (c *Cache) Put(ctx context.Context, ev verify.Evidence) error {
	if !platform.ValidDigest(ev.Digest) {
		return fmt.Errorf("publish: invalid digest %q", ev.Digest)
	}
	if ev.Provenance.Status == verify.Unchecked {
		return nil
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("publish: encode %s: %w", ev.Digest, err)
	}
	key := verifiedKey(ev.Digest)
	_, err = c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(jsonContentType),
	})
	if err != nil {
		return fmt.Errorf("publish: put %s: %w", key, err)
	}
	return nil
}

func verifiedKey(digest string) string { return verifiedPrefix + digest + ".json" }

func isNotFound(err error) bool {
	var noSuchKey *types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode() == "AccessDenied" {
		return true
	}
	var respErr *smithyhttp.ResponseError
	if errors.As(err, &respErr) {
		switch respErr.HTTPStatusCode() {
		case http.StatusNotFound, http.StatusForbidden:
			return true
		}
	}
	return false
}
