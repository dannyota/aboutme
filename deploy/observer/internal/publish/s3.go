// Package publish writes the deployment document and the verification cache
// to one S3 bucket (docs/design/deployment-transparency/README.md, "Serving
// and caching"; docs/design/deployment-transparency/verification.md, "What
// verified means").
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

	"github.com/dannyota/aboutme/deploy/observer/internal/platform"
	"github.com/dannyota/aboutme/deploy/observer/internal/verify"
)

// documentKey is the published document's fixed key. CloudFront appends the
// viewer's path to the S3 origin, so the key matches the served path
// exactly (docs/design/deployment-transparency/README.md, "Serving and
// caching").
const documentKey = ".well-known/deployment.json"

// verifiedPrefix holds one cached verify.Evidence per digest.
const verifiedPrefix = "verified/"

const jsonContentType = "application/json"

// maxCacheEntryBytes bounds one cached verify.Evidence read back.
const maxCacheEntryBytes = 64 << 10

// documentCacheControl matches the edge cache policy
// (docs/design/deployment-transparency/README.md, "Serving and caching").
const documentCacheControl = "public, max-age=30"

// NewClient returns an S3 client for cfg. When endpoint is not empty, the
// client addresses it directly with path-style requests, for an
// S3-compatible store instead of Amazon S3.
func NewClient(cfg aws.Config, endpoint string) *s3.Client {
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		}
	})
}

// Publisher writes the deployment document to one bucket.
type Publisher struct {
	client *s3.Client
	bucket string
}

// NewPublisher returns a Publisher writing to bucket through client.
func NewPublisher(client *s3.Client, bucket string) *Publisher {
	return &Publisher{client: client, bucket: bucket}
}

// Put writes body as the published document.
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

// Cache implements verify.Cache on the same bucket's verified/<digest>.json
// keys.
type Cache struct {
	client *s3.Client
	bucket string
}

// NewCache returns a Cache reading and writing bucket through client.
func NewCache(client *s3.Client, bucket string) *Cache {
	return &Cache{client: client, bucket: bucket}
}

// Get implements verify.Cache.
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

// Put implements verify.Cache. It refuses to store an Unchecked result or a
// malformed digest (docs/design/deployment-transparency/verification.md,
// "What verified means").
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

// isNotFound reports whether err is S3's NoSuchKey, a plain HTTP 404 as an
// S3-compatible store may answer, or AccessDenied (HTTP 403). The
// observer's role has no s3:ListBucket, so a GetObject on a missing
// verified/<digest>.json key comes back as AccessDenied rather than
// NoSuchKey (docs/design/deployment-transparency/README.md, "Access on
// AWS"): S3 refuses to disclose whether a key the caller cannot list exists.
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
