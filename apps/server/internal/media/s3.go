package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// S3Config configures the S3-compatible backend. Endpoint is empty for
// real AWS (ECS task-role default credential chain, virtual-hosted
// addressing, no static keys) and set for the local S3-compatible service
// (path-style addressing and one complete disposable static key pair
// required). Secret values are never logged, echoed in an error, or
// included in any returned value.
type S3Config struct {
	Bucket          string
	Region          string
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	ForcePathStyle  bool
}

// s3Backend is the S3-compatible implementation of Backend.
//
// Error hygiene: remote error codes and messages are attacker-influencable
// response text and can echo request credentials, so no SDK or transport
// error is ever wrapped into a returned error. Returned errors carry only
// this package's own words, the validated key, the numeric HTTP status
// when a response was received, and standard-library context sentinels.
type s3Backend struct {
	client *s3.Client
	bucket string
}

// NewS3 returns the S3-compatible backend. Endpoint is empty for real AWS
// and set for the local service; PathStyle is required for the latter.
func NewS3(ctx context.Context, cfg S3Config) (Backend, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("media: s3 bucket is required")
	}
	if strings.TrimSpace(cfg.Region) == "" {
		return nil, errors.New("media: s3 region is required")
	}

	if cfg.Endpoint == "" {
		// AWS mode: the ECS task-role default credential chain. Static
		// keys and path-style addressing are custom-endpoint mechanisms;
		// their presence here is a misconfiguration that must fail closed
		// rather than silently shadow the intended credential source.
		if cfg.AccessKeyID != "" || cfg.SecretAccessKey != "" {
			return nil, errors.New("media: s3 static access keys require a custom endpoint; unset them for AWS mode")
		}
		if cfg.ForcePathStyle {
			return nil, errors.New("media: s3 path-style addressing requires a custom endpoint")
		}
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
		if err != nil {
			return nil, errors.New("media: loading the default AWS configuration failed")
		}
		return &s3Backend{client: s3.NewFromConfig(awsCfg), bucket: cfg.Bucket}, nil
	}

	// Custom-endpoint mode: the local S3-compatible service.
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("media: s3 endpoint must be an absolute http(s) URL")
	}
	if u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("media: s3 endpoint must be scheme://host[:port] only — no userinfo, path, query, or fragment")
	}
	if !cfg.ForcePathStyle {
		return nil, errors.New("media: a custom s3 endpoint requires path-style addressing")
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errors.New("media: a custom s3 endpoint requires a complete access-key/secret pair")
	}
	awsCfg := aws.Config{
		Region:      cfg.Region,
		Credentials: credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.Endpoint)
		o.UsePathStyle = true
	})
	return &s3Backend{client: client, bucket: cfg.Bucket}, nil
}

// Put implements Backend with S3 create-only conditional writes.
func (b *s3Backend) Put(ctx context.Context, key, contentType string, body io.Reader, size int64) (PutOutcome, error) {
	if err := validatePut(ctx, key, contentType, size); err != nil {
		return PutNotCreated, err
	}
	// Prove the body's EOF sits exactly at size before dispatch, so a bad
	// body is a proved non-create with no request on the wire, and the
	// seekable buffer lets the SDK sign the payload.
	buf, err := readExact(body, size)
	if err != nil {
		return PutNotCreated, err
	}
	if contextErr := ctx.Err(); contextErr != nil {
		// Canceled after body read but still before dispatch.
		return PutNotCreated, contextErr
	}

	_, err = b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(b.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(buf),
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
		// Conditional create (ADR 0019): the write commits only if no
		// object exists at the key. No overwrite path exists.
		IfNoneMatch: aws.String("*"),
	}, func(o *s3.Options) {
		// A retry after an ambiguous failure could collide with this
		// call's own earlier success and misreport a proved non-create,
		// so the conditional write is dispatched exactly once and any
		// ambiguity surfaces as PutUnknown instead.
		o.Retryer = aws.NopRetryer{}
	})
	if err == nil {
		return PutCreated, nil
	}

	if status, ok := responseStatus(err); ok {
		switch {
		case status == 412:
			// The service proved the conditional create did not occur
			// because the key already names an object.
			return PutNotCreated, fmt.Errorf("media: s3 put %q: %w", key, ErrAlreadyExists)
		case status >= 400 && status < 500:
			// A received rejection: the service refused the request, so
			// this call created nothing — but existing bytes at the key
			// are not proved (e.g. ConditionalRequestConflict), so this
			// is not ErrAlreadyExists.
			return PutNotCreated, fmt.Errorf("media: s3 put %q: service rejected the request (http %d)", key, status)
		default:
			// A 5xx response does not prove whether the write committed.
			return PutUnknown, fmt.Errorf("media: s3 put %q: outcome unknown (http %d)", key, status)
		}
	}
	// No conclusive service result: transport loss or cancellation after
	// the request may have reached the service. The key may name this
	// call's object or a collision winner's; only reconciliation may ever
	// delete it (D18).
	if ctxErr := ctx.Err(); ctxErr != nil {
		return PutUnknown, fmt.Errorf("media: s3 put %q: outcome unknown, canceled after dispatch: %w", key, ctxErr)
	}
	return PutUnknown, fmt.Errorf("media: s3 put %q: outcome unknown, no service response", key)
}

// Get implements Backend for one private S3 object.
func (b *s3Backend) Get(ctx context.Context, key string) (io.ReadCloser, string, error) {
	if err := validateKey(key); err != nil {
		return nil, "", err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNoSuchKey(err) {
			return nil, "", ErrNotFound
		}
		return nil, "", opError("get", key, err)
	}
	return out.Body, aws.ToString(out.ContentType), nil
}

// Delete implements Backend for one exact S3 object key.
func (b *s3Backend) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// S3 DeleteObject is idempotent and normally returns success even when
	// the key is absent. Probe first so this backend can uphold the shared
	// contract's explicit ErrNotFound result. A concurrent deletion after a
	// successful probe is harmless: the following exact-key delete remains
	// idempotent and no unrelated object can be affected.
	_, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNoSuchKey(err) {
			return ErrNotFound
		}
		return opError("delete", key, err)
	}
	_, err = b.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(b.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNoSuchKey(err) {
			return ErrNotFound
		}
		return opError("delete", key, err)
	}
	return nil
}

// ListPage implements Backend with stable key-ordered S3 pages.
func (b *s3Backend) ListPage(ctx context.Context, prefix, cursor string, limit int) ([]Object, string, error) {
	if err := validateListPage(ctx, prefix, cursor, limit); err != nil {
		return nil, "", err
	}
	input := &s3.ListObjectsV2Input{
		Bucket:  aws.String(b.bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int32(int32(limit)), //nolint:gosec // validateListPage bounds limit to a small positive page size.
	}
	if cursor != "" {
		// StartAfter rather than an opaque continuation token: the cursor
		// is then a plain key on both backends, so pages stay stable and
		// resumable across processes (the orphan sweep's stored cursor).
		input.StartAfter = aws.String(cursor)
	}
	out, err := b.client.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, "", opError("list", prefix, err)
	}
	if len(out.Contents) > limit {
		return nil, "", errors.New("media: s3 list response exceeded the requested limit")
	}
	if aws.ToBool(out.IsTruncated) && len(out.Contents) != limit {
		return nil, "", errors.New("media: s3 list response marked a short page as truncated")
	}
	objects := make([]Object, 0, len(out.Contents))
	previous := cursor
	for _, c := range out.Contents {
		key := aws.ToString(c.Key)
		updatedAt := aws.ToTime(c.LastModified)
		if validateKey(key) != nil || !strings.HasPrefix(key, prefix) {
			return nil, "", errors.New("media: s3 list response contained an invalid or out-of-scope key")
		}
		if previous != "" && key <= previous {
			return nil, "", errors.New("media: s3 list response did not advance in key order")
		}
		if updatedAt.IsZero() {
			return nil, "", errors.New("media: s3 list response omitted an object update time")
		}
		objects = append(objects, Object{Key: key, UpdatedAt: updatedAt})
		previous = key
	}
	nextCursor := ""
	if aws.ToBool(out.IsTruncated) && len(objects) > 0 {
		nextCursor = objects[len(objects)-1].Key
	}
	return objects, nextCursor, nil
}

// responseStatus extracts the HTTP status when the service answered, i.e.
// when a response was actually received for the request.
func responseStatus(err error) (int, bool) {
	var respErr *awshttp.ResponseError
	if errors.As(err, &respErr) {
		return respErr.HTTPStatusCode(), true
	}
	return 0, false
}

// isNoSuchKey reports the absent-object answers: the typed NoSuchKey /
// NotFound errors or any bare 404 response.
func isNoSuchKey(err error) bool {
	var noSuchKey *types.NoSuchKey
	var notFound *types.NotFound
	if errors.As(err, &noSuchKey) || errors.As(err, &notFound) {
		return true
	}
	if status, ok := responseStatus(err); ok && status == 404 {
		// A 404 whose code is about the bucket, not the key, is a real
		// configuration error, not object absence.
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && strings.EqualFold(apiErr.ErrorCode(), "NoSuchBucket") {
			return false
		}
		return true
	}
	return false
}

// opError renders a remote failure without ever including SDK, service, or
// transport error text (see s3Backend's error-hygiene comment). Context
// sentinels are preserved for errors.Is.
func opError(op, subject string, err error) error {
	if status, ok := responseStatus(err); ok {
		return fmt.Errorf("media: s3 %s %q: service error (http %d)", op, subject, status)
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("media: s3 %s %q: %w", op, subject, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("media: s3 %s %q: %w", op, subject, context.DeadlineExceeded)
	}
	return fmt.Errorf("media: s3 %s %q: transport failure, no service response", op, subject)
}
