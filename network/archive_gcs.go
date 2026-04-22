package network

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

// gcsArchive is the Google Cloud Storage backend.
//
// Auth: defaults to Application Default Credentials. On k8s this is
// Workload Identity; for local dev set GOOGLE_APPLICATION_CREDENTIALS
// to a service-account JSON. We never read credentials from config
// strings — they come from the environment.
type gcsArchive struct {
	*archiveWriter
	client *storage.Client
	bucket string
}

type gcsUpload struct {
	client *storage.Client
	bucket string
}

func newGCSArchive(ctx context.Context, bucket, svcPrefix string, cfg ArchiveConfig, m *ArchiveMetrics) (*gcsArchive, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("archive gcs: new client: %w", err)
	}
	// Sanity-check the bucket exists and we can reach it. Attrs returns
	// a typed error if the bucket is missing so we can surface a clear
	// diagnostic rather than silently buffering forever.
	if _, err := client.Bucket(bucket).Attrs(ctx); err != nil {
		_ = client.Close()
		if errors.Is(err, storage.ErrBucketNotExist) {
			return nil, fmt.Errorf("archive gcs: bucket %q does not exist", bucket)
		}
		return nil, fmt.Errorf("archive gcs: bucket %q attrs: %w", bucket, err)
	}
	up := &gcsUpload{client: client, bucket: bucket}
	return &gcsArchive{
		archiveWriter: newArchiveWriter(up, svcPrefix, cfg, m),
		client:        client,
		bucket:        bucket,
	}, nil
}

// Append satisfies Archive.
func (a *gcsArchive) Append(ctx context.Context, shardID string, seq uint64, frame []byte) error {
	return a.archiveWriter.Append(ctx, shardID, seq, frame)
}

// Range satisfies Archive.
func (a *gcsArchive) Range(ctx context.Context, shardID string, fromSeq, toSeq uint64) (iter.Seq2[Frame, error], error) {
	return a.archiveWriter.Range(ctx, shardID, fromSeq, toSeq)
}

// Close satisfies Archive. Closes the GCS client after the writer
// drains so in-flight uploads aren't interrupted.
func (a *gcsArchive) Close() error {
	// archiveWriter.Close() calls u.close() which closes the client.
	return a.archiveWriter.Close()
}

// --- uploader impl ---

func (u *gcsUpload) put(ctx context.Context, key string, body []byte) error {
	w := u.client.Bucket(u.bucket).Object(key).NewWriter(ctx)
	w.ContentType = "application/octet-stream"
	if _, err := io.Copy(w, bytes.NewReader(body)); err != nil {
		_ = w.Close()
		return fmt.Errorf("gcs put %s/%s: %w", u.bucket, key, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("gcs put close %s/%s: %w", u.bucket, key, err)
	}
	return nil
}

func (u *gcsUpload) get(ctx context.Context, key string) ([]byte, error) {
	r, err := u.client.Bucket(u.bucket).Object(key).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcs get %s/%s: %w", u.bucket, key, err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("gcs read %s/%s: %w", u.bucket, key, err)
	}
	return b, nil
}

func (u *gcsUpload) list(ctx context.Context, prefix string) ([]string, error) {
	var out []string
	it := u.client.Bucket(u.bucket).Objects(ctx, &storage.Query{Prefix: prefix})
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("gcs list %s/%s: %w", u.bucket, prefix, err)
		}
		out = append(out, attrs.Name)
	}
	return out, nil
}

func (u *gcsUpload) close() error { return u.client.Close() }

func (u *gcsUpload) scheme() string { return "gs" }
