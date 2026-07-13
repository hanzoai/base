package network

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/hanzoai/base/internal/s3"
)

// s3Archive is the S3-protocol replicate backend. It talks plain S3 over
// Base's own dependency-free SigV4 client (github.com/hanzoai/base/internal/s3
// — the same client Base's file storage uses), so it works against the
// canonical hanzoai/s3 (SeaweedFS) object store, AWS S3, MinIO, or any other
// S3-compatible endpoint. No minio-go, no aws-sdk-go: one S3 client for the
// whole codebase.
//
// Credential resolution:
//   - AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY static creds (required).
//     This matches Base's file-storage credential model — secrets arrive as
//     env from KMS. There is no AWS-only IAM/IRSA/EC2-metadata chain here
//     (Base runs on DOKS, not EKS, and its primary S3 client is static-only);
//     one credential model, everywhere.
//
// Endpoint resolution:
//   - AWS_ENDPOINT_URL set      → that endpoint (hanzoai/s3 / MinIO / ...).
//   - AWS_ENDPOINT_URL unset    → s3.<region>.amazonaws.com default.
//
// Both paths honour AWS_REGION (defaults to us-east-1). Path-style addressing
// is always used: it is required by hanzoai/s3 and MinIO and accepted by AWS,
// so a single addressing mode works against every backend.
type s3Archive struct {
	*archiveWriter
	client *s3.S3
	bucket string
}

type s3Upload struct {
	client *s3.S3
	bucket string
}

func newS3Archive(ctx context.Context, bucket, svcPrefix string, cfg ArchiveConfig, m *ArchiveMetrics) (*s3Archive, error) {
	endpoint, err := resolveS3Endpoint()
	if err != nil {
		return nil, err
	}
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}
	ak, sk, err := resolveS3Creds()
	if err != nil {
		return nil, err
	}
	client := &s3.S3{
		Bucket:       bucket,
		Region:       region,
		Endpoint:     endpoint,
		AccessKey:    ak,
		SecretKey:    sk,
		UsePathStyle: true,
	}

	// Fail fast if the bucket is unreachable/missing. A ListObjects probe
	// with MaxKeys=1 is the client-agnostic equivalent of BucketExists: a
	// 404 means the bucket does not exist; any other error is a
	// connectivity/auth failure.
	if _, err := client.ListObjects(ctx, s3.ListParams{MaxKeys: 1}); err != nil {
		var re *s3.ResponseError
		if errors.As(err, &re) && re.Status == http.StatusNotFound {
			return nil, fmt.Errorf("archive s3: bucket %q does not exist", bucket)
		}
		return nil, fmt.Errorf("archive s3: bucket check %s: %w", bucket, err)
	}

	up := &s3Upload{client: client, bucket: bucket}
	return &s3Archive{
		archiveWriter: newArchiveWriter(up, svcPrefix, cfg, m),
		client:        client,
		bucket:        bucket,
	}, nil
}

// Append satisfies Archive.
func (a *s3Archive) Append(ctx context.Context, shardID string, seq uint64, frame []byte) error {
	return a.archiveWriter.Append(ctx, shardID, seq, frame)
}

// Range satisfies Archive.
func (a *s3Archive) Range(ctx context.Context, shardID string, fromSeq, toSeq uint64) (iter.Seq2[Frame, error], error) {
	return a.archiveWriter.Range(ctx, shardID, fromSeq, toSeq)
}

// Close satisfies Archive.
func (a *s3Archive) Close() error { return a.archiveWriter.Close() }

// --- uploader impl ---

func (u *s3Upload) put(ctx context.Context, key string, body []byte) error {
	uploader := &s3.Uploader{
		S3:      u.client,
		Key:     key,
		Payload: bytes.NewReader(body),
	}
	err := uploader.Upload(ctx, func(r *http.Request) {
		r.Header.Set("Content-Type", "application/octet-stream")
	})
	if err != nil {
		return fmt.Errorf("s3 put %s/%s: %w", u.bucket, key, err)
	}
	return nil
}

func (u *s3Upload) get(ctx context.Context, key string) ([]byte, error) {
	resp, err := u.client.GetObject(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("s3 get %s/%s: %w", u.bucket, key, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("s3 read %s/%s: %w", u.bucket, key, err)
	}
	return b, nil
}

func (u *s3Upload) list(ctx context.Context, prefix string) ([]string, error) {
	var out []string
	var token string
	for {
		resp, err := u.client.ListObjects(ctx, s3.ListParams{
			Prefix:            prefix,
			ContinuationToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("s3 list %s/%s: %w", u.bucket, prefix, err)
		}
		for _, obj := range resp.Contents {
			out = append(out, obj.Key)
		}
		if !resp.IsTruncated || resp.NextContinuationToken == "" {
			break
		}
		token = resp.NextContinuationToken
	}
	return out, nil
}

func (u *s3Upload) close() error { return nil }

func (u *s3Upload) scheme() string { return "s3" }

// resolveS3Endpoint picks the endpoint (scheme://host[:port]). AWS_ENDPOINT_URL
// lets operators point at hanzoai/s3, MinIO, or any other S3-compatible
// endpoint; without it, we talk to AWS. The returned value always carries an
// explicit scheme so the S3 client's TLS decision is unambiguous.
func resolveS3Endpoint() (string, error) {
	raw := os.Getenv("AWS_ENDPOINT_URL")
	if raw == "" {
		region := os.Getenv("AWS_REGION")
		if region == "" {
			region = "us-east-1"
		}
		return fmt.Sprintf("https://s3.%s.amazonaws.com", region), nil
	}
	// Accept both "host:port" and full URLs; default to https when no scheme.
	if !strings.Contains(raw, "://") {
		return "https://" + raw, nil
	}
	if _, err := url.Parse(raw); err != nil {
		return "", fmt.Errorf("archive s3: parse AWS_ENDPOINT_URL %q: %w", raw, err)
	}
	return raw, nil
}

// resolveS3Creds returns the static access/secret key pair. Static creds are
// required — this matches Base's file-storage credential model (secrets from
// KMS as env). Missing creds fail closed rather than silently attempting an
// unsupported credential chain.
func resolveS3Creds() (string, string, error) {
	ak := os.Getenv("AWS_ACCESS_KEY_ID")
	sk := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if ak == "" || sk == "" {
		return "", "", errors.New("archive s3: AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY are required")
	}
	return ak, sk, nil
}
