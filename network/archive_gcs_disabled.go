//go:build !gcs

// Default-build stub for the GCS archive backend.
//
// cloud.google.com/go/storage pulls google.golang.org/grpc + googleapis +
// google/s2a-go + cloud.google.com/go/iam + the rest of the Google Cloud
// SDK transitive chain. The grpc dep is opt-in only on the o11y/Lux side
// (build with `-tags grpc`), so GCS support follows the same discipline:
// opt-in via `-tags gcs` to flip on the real backend in archive_gcs.go.
//
// Without the tag, `gs://` archive URLs return an error pointing the
// operator at S3 (s3://) — every Lux/Hanzo deploy ships MinIO-protocol
// S3 (hanzoai/s3) for sovereign storage, so the disabled case is a
// configuration mistake, not a missing-feature complaint.
package network

import (
	"context"
	"errors"
	"iter"
)

var errGCSDisabled = errors.New(
	"network/archive: gs:// scheme not compiled (build with `-tags gcs` to enable Google Cloud Storage; default deploys use s3:// via hanzoai/s3)",
)

// gcsArchive is the disabled-build type. newGCSArchive always returns
// errGCSDisabled so the methods are never actually called; the Archive
// interface satisfaction below is purely to keep the dispatch in
// archive.go type-checking.
type gcsArchive struct{}

func newGCSArchive(_ context.Context, _, _ string, _ ArchiveConfig, _ *ArchiveMetrics) (*gcsArchive, error) {
	return nil, errGCSDisabled
}

func (*gcsArchive) Append(_ context.Context, _ string, _ uint64, _ []byte) error {
	return errGCSDisabled
}

func (*gcsArchive) Range(_ context.Context, _ string, _, _ uint64) (iter.Seq2[Frame, error], error) {
	return nil, errGCSDisabled
}

func (*gcsArchive) Close() error { return errGCSDisabled }
