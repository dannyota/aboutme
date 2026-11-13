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

type S3Config struct {
	Bucket          string
	Region          string
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	ForcePathStyle  bool
}

type s3Backend struct {
	client *s3.Client
	bucket string
}

func NewS3(ctx context.Context, cfg S3Config) (Backend, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("media: s3 bucket is required")
	}
	if strings.TrimSpace(cfg.Region) == "" {
		return nil, errors.New("media: s3 region is required")
	}

	if cfg.Endpoint == "" {
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

func (b *s3Backend) Put(ctx context.Context, key, contentType string, body io.Reader, size int64) (PutOutcome, error) {
	if err := validatePut(ctx, key, contentType, size); err != nil {
		return PutNotCreated, err
	}
	// The AWS SDK needs a seekable body to sign the payload.
	buf, err := readExact(body, size)
	if err != nil {
		return PutNotCreated, err
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return PutNotCreated, contextErr
	}

	_, err = b.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(b.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(buf),
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
		IfNoneMatch:   aws.String("*"),
	}, func(o *s3.Options) {
		o.Retryer = aws.NopRetryer{}
	})
	if err == nil {
		return PutCreated, nil
	}

	if status, ok := responseStatus(err); ok {
		switch {
		case status == 412:
			return PutNotCreated, fmt.Errorf("media: s3 put %q: %w", key, ErrAlreadyExists)
		case status >= 400 && status < 500:
			return PutNotCreated, fmt.Errorf("media: s3 put %q: service rejected the request (http %d)", key, status)
		default:
			return PutUnknown, fmt.Errorf("media: s3 put %q: outcome unknown (http %d)", key, status)
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return PutUnknown, fmt.Errorf("media: s3 put %q: outcome unknown, canceled after dispatch: %w", key, ctxErr)
	}
	return PutUnknown, fmt.Errorf("media: s3 put %q: outcome unknown, no service response", key)
}

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

func (b *s3Backend) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// S3 DeleteObject succeeds even when the key is absent, so probe first to report ErrNotFound.
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

func responseStatus(err error) (int, bool) {
	var respErr *awshttp.ResponseError
	if errors.As(err, &respErr) {
		return respErr.HTTPStatusCode(), true
	}
	return 0, false
}

func isNoSuchKey(err error) bool {
	var noSuchKey *types.NoSuchKey
	var notFound *types.NotFound
	if errors.As(err, &noSuchKey) || errors.As(err, &notFound) {
		return true
	}
	if status, ok := responseStatus(err); ok && status == 404 {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && strings.EqualFold(apiErr.ErrorCode(), "NoSuchBucket") {
			return false
		}
		return true
	}
	return false
}

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
