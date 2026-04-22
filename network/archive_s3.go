package network

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"net/url"
	"os"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// s3Archive is the S3/MinIO-protocol backend. It works against AWS S3,
// hanzoai/s3 self-hosted, and any other MinIO-compatible endpoint.
//
// Credential resolution order (first match wins):
//  1. AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY static creds.
//  2. IAM / IRSA / Workload Identity chain (credentials.IAM).
//
// Endpoint resolution:
//  - AWS_ENDPOINT_URL set      → that endpoint (MinIO / hanzoai/s3).
//  - AWS_ENDPOINT_URL unset    → s3.<region>.amazonaws.com default.
//
// Both paths honour AWS_REGION (defaults to us-east-1).
type s3Archive struct {
	*archiveWriter
	client *minio.Client
	bucket string
}

type s3Upload struct {
	client *minio.Client
	bucket string
}

func newS3Archive(ctx context.Context, bucket, svcPrefix string, cfg ArchiveConfig, m *ArchiveMetrics) (*s3Archive, error) {
	endpoint, secure, err := resolveS3Endpoint()
	if err != nil {
		return nil, err
	}
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}
	creds, err := resolveS3Creds()
	if err != nil {
		return nil, err
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  creds,
		Secure: secure,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("archive s3: new client: %w", err)
	}
	ok, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("archive s3: bucket check %s: %w", bucket, err)
	}
	if !ok {
		return nil, fmt.Errorf("archive s3: bucket %q does not exist", bucket)
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
	_, err := u.client.PutObject(ctx, u.bucket, key, bytes.NewReader(body), int64(len(body)), minio.PutObjectOptions{
		ContentType:    "application/octet-stream",
		SendContentMd5: true,
	})
	if err != nil {
		return fmt.Errorf("s3 put %s/%s: %w", u.bucket, key, err)
	}
	return nil
}

func (u *s3Upload) get(ctx context.Context, key string) ([]byte, error) {
	obj, err := u.client.GetObject(ctx, u.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("s3 get %s/%s: %w", u.bucket, key, err)
	}
	defer obj.Close()
	b, err := io.ReadAll(obj)
	if err != nil {
		return nil, fmt.Errorf("s3 read %s/%s: %w", u.bucket, key, err)
	}
	return b, nil
}

func (u *s3Upload) list(ctx context.Context, prefix string) ([]string, error) {
	var out []string
	for info := range u.client.ListObjects(ctx, u.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if info.Err != nil {
			return nil, fmt.Errorf("s3 list %s/%s: %w", u.bucket, prefix, info.Err)
		}
		out = append(out, info.Key)
	}
	return out, nil
}

func (u *s3Upload) close() error { return nil }

func (u *s3Upload) scheme() string { return "s3" }

// resolveS3Endpoint picks the endpoint host:port and TLS flag.
// AWS_ENDPOINT_URL lets operators point at hanzoai/s3, MinIO, or any
// other S3-compatible endpoint; without it, we talk to AWS.
func resolveS3Endpoint() (string, bool, error) {
	raw := os.Getenv("AWS_ENDPOINT_URL")
	if raw == "" {
		// AWS default.
		region := os.Getenv("AWS_REGION")
		if region == "" {
			region = "us-east-1"
		}
		return fmt.Sprintf("s3.%s.amazonaws.com", region), true, nil
	}
	// Accept both "host:port" and full URLs.
	if !strings.Contains(raw, "://") {
		return raw, true, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false, fmt.Errorf("archive s3: parse AWS_ENDPOINT_URL %q: %w", raw, err)
	}
	return u.Host, u.Scheme == "https", nil
}

// resolveS3Creds returns a provider chain. Static creds win if both
// env vars are set; otherwise we delegate to IAM / IRSA / Workload
// Identity via the minio-go credentials.IAM provider.
func resolveS3Creds() (*credentials.Credentials, error) {
	ak := os.Getenv("AWS_ACCESS_KEY_ID")
	sk := os.Getenv("AWS_SECRET_ACCESS_KEY")
	token := os.Getenv("AWS_SESSION_TOKEN")
	if ak != "" && sk != "" {
		return credentials.NewStaticV4(ak, sk, token), nil
	}
	// IAM chain: EKS/IRSA via WebIdentity, EC2 metadata, ECS task role.
	return credentials.NewIAM(""), nil
}
